package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"adcenter/internal/config"
	"adcenter/internal/store"
)

// ClickContext 一次点击的归因上下文：点击发生时服务端快照并登记
// （app/style/advertiser/device/creative），S2S 转化回调凭 clickid 反查还原。
// 归属只信服务端自己的点击登记，不信回调自报归属参数（防伪造归因）。
type ClickContext struct {
	AppID, AdvertiserID, Style string
	DeviceID, CreativeID       string
}

// ClickResolver 按 clickid 还原点击上下文（点击登记表的存储实现）。
//
// clickid 由 POST /v1/ad/click 生成并落 ads_center.clicks（store.RegisterClick），
// 归属只信服务端自己的点击登记，不信回调自报归属参数（防伪造归因）。
// Server.Clicks 未注入（nil）时 S2S 仍只确认不扣费（兜底）。
type ClickResolver interface {
	Resolve(clickID string) (*ClickContext, bool)
}

// handleS2SEvent 归因方 S2S 转化回调（同时支持 GET 与 POST，用户决定暂不加独立密钥）。
//
// 客户端不应（也无法）上报转化：安装/激活/注册/首充/充值只能由归因平台
// （Adjust/AppsFlyer…）或广告主服务器确认后通知。此回调与 Adjust 的转化
// 回调参数对齐，参数由归因方模板宏展开回传：
//
//	clickid     必填。投放时为一次点击生成的唯一 ID；Adjust 点击事件存下后，
//	             转化回调用 {clickid} 宏原样带回。服务端凭它反查点击上下文
//	             （ClickResolver）。具体透传方式（素材跳转链接参数等）待定。
//	event_name  必填。install / activate / register / first_purchase / purchase
//	pixelId     归因方/中介（berealads）的转化像素/任务标识。落库到
//	             ad_conversions.pixel_id 便于"按中介侧任务标记"查看与统计；不参与
//	             归属（归属只信 clickid 反查）。建议 berealads 侧配置为广告任务 id，
//	             与服务端 campaign_id 对照。
//	testFlag    归因方测试流量标记（1/true…）：测试回调不扣费不记账。
//	currency    value 的币种（ISO 4217）；充值事件（first_purchase/purchase）回传。
//	value       充值流水金额；非充值事件为 0。当前 cpa 计费按 cpa_event_prices
//	             固定单价，value 只记录不参与扣费（将来若广告主按流水抽成，
//	             计费改为 value×比例时从这里取）。
//
// 扣费：金额完全由服务端按 advertisers.cpa_event_prices 计算，不信回调上报。
// 归属：只信 clickid 反查结果；归因不到（登记表未上线/点击过期不存在）→
// 200 确认但不扣费不记账（防归因方把失败当重试放大刷量），日志暴露未归因量。
func (s *Server) handleS2SEvent(w http.ResponseWriter, r *http.Request) {
	q := s2sParams(r)
	clickID := q.Get("clickid")
	rawEventName := q.Get("event_name")
	eventName := normalizeEventName(rawEventName)
	pixelID := q.Get("pixelId")
	currency := q.Get("currency")
	valueStr := q.Get("value")

	if clickID == "" {
		writeError(w, http.StatusBadRequest, "clickid required")
		return
	}
	if !slices.Contains(config.ConversionEvents, eventName) {
		// 事件名解析失败会直接导致 cpa 计费永不触发：把原始值打出来，便于对接排查。
		s.Log.Warn("s2s unknown event_name",
			"raw_event_name", rawEventName, "clickid", clickID, "pixel_id", pixelID)
		writeError(w, http.StatusBadRequest,
			"event_name must be one of "+strings.Join(config.ConversionEvents, "/")+
				" (got "+rawEventName+")")
		return
	}
	if isTruthy(q.Get("testFlag")) {
		// 归因方测试流量：确认收到，不扣费不记账
		writeJSON(w, http.StatusOK, map[string]string{"status": "test"})
		return
	}
	var value float64
	if valueStr != "" {
		v, err := strconv.ParseFloat(valueStr, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "value must be a number")
			return
		}
		value = v
	}

	ctx, ok := s.resolveClick(clickID)
	if !ok {
		// 归因不到：clickid 登记表未接入（或点击过期/不存在）。确认收到，
		// 不扣费不记账；日志暴露未归因转化（clickid 机制上线后应收敛为 0）。
		s.Log.Warn("s2s unattributable conversion", "clickid", clickID,
			"event_name", eventName, "pixel_id", pixelID,
			"currency", currency, "value", value)
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	// 按点击登记时的广告主扣费（只有 billing_mode=cpa 且该事件有单价才扣）
	now := time.Now()
	snap := s.Cache.Snapshot()
	var charged float64
	var campaignID string
	// 计费执行粒度 = campaign：出价 / 计费方式 / CPA 单价均在任务级，广告主只做钱包。
	if campID, ok := snap.CreativeCampaign[ctx.CreativeID]; ok && campID != "" {
		campaignID = campID
		if camp := snap.Campaigns[campID]; camp != nil {
			if amt, ok := camp.BillingAmount(eventName); ok {
				// campaign 日预算闸 + 广告主总钱包闸：任一不足即不扣费
				if s.Budget.TryDeduct(campID, amt) && s.Budget.WalletDeduct(ctx.AdvertiserID, amt) {
					charged = amt
				}
			}
		}
	}
	s.Metrics.RecordEvent(ctx.AppID, ctx.Style, ctx.AdvertiserID, eventName, charged, now)
	s.enqueueEvent(store.AdEvent{
		AppID: ctx.AppID, Style: ctx.Style, AdvertiserID: ctx.AdvertiserID,
		DeviceID: ctx.DeviceID, CreativeID: ctx.CreativeID,
		EventType: eventName, Revenue: charged,
		CampaignID: campaignID, PixelID: pixelID,
		ClickID: clickID, Currency: currency, Value: value,
	})
	s.Log.Info("s2s conversion", "clickid", clickID, "event_name", eventName,
		"advertiser_id", ctx.AdvertiserID, "charged", charged,
		"currency", currency, "value", value)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// resolveClick 反查点击上下文；未注入 ClickResolver（nil）时恒返回不可归因（兜底）。
func (s *Server) resolveClick(clickID string) (*ClickContext, bool) {
	if s.Clicks == nil {
		return nil, false
	}
	return s.Clicks.Resolve(clickID)
}

// isTruthy 解析归因方 testFlag 等布尔参数（true/1/yes/on 不区分大小写）。
func isTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// s2sParams 合并取回调参数：GET 走 query；POST 额外解析 body（query 优先）。
// 兼容 JSON（application/json，含数字/布尔值）与表单（application/x-www-form-urlencoded）。
func s2sParams(r *http.Request) url.Values {
	params := r.URL.Query()
	if r.Method != http.MethodPost {
		return params
	}
	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
		dec.UseNumber() // 数字保留原文，避免 float64 科学计数法/精度问题
		var body map[string]any
		if err := dec.Decode(&body); err != nil {
			return params
		}
		for k, v := range body {
			if params.Get(k) != "" {
				continue // query 优先，body 仅补齐 query 未提供的键
			}
			switch t := v.(type) {
			case string:
				params.Set(k, t)
			case json.Number:
				params.Set(k, t.String())
			case bool:
				params.Set(k, strconv.FormatBool(t))
			}
		}
		return params
	}
	// 表单：r.Form 已含 query + body，PostForm 为 body 部分
	if err := r.ParseForm(); err == nil {
		for k, vs := range r.PostForm {
			if params.Get(k) == "" && len(vs) > 0 {
				params.Set(k, vs[0])
			}
		}
	}
	return params
}

// s2sEventAliases 归因方/中介回传的固定事件枚举 → 服务端 ConversionEvents。
// 中介侧写死了 EVENT_* 命名，且与我们的枚举**不是简单前缀关系**
// （EVENT_REGISTRATION→register、EVENT_FIRST_DEPOSIT→first_purchase、
// EVENT_APP_ACTIVATE→activate），必须显式映射；否则事件名对不上，
// BillingAmount 永远匹配不到 → cpa 计费永不触发（钱扣不到）。
var s2sEventAliases = map[string]string{
	"event_install":       "install",
	"event_registration":  "register",
	"event_purchase":      "purchase",
	"event_first_deposit": "first_purchase",
	"event_subscribe":     "subscribe",
	"event_app_activate":  "activate",
}

// normalizeEventName 把归因方/中介回传的事件名归一为服务端枚举（config.ConversionEvents）：
// 先查显式别名表（兼容中介写死的 EVENT_* 命名），未命中再做通用归一
// （忽略大小写、去掉可选 event_ 前缀、-/空格 折成 _），以兼容其它平台。
func normalizeEventName(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	if v, ok := s2sEventAliases[s]; ok {
		return v
	}
	s = strings.TrimPrefix(s, "event_")
	s = strings.NewReplacer("-", "_", " ", "_").Replace(s)
	s = strings.ReplaceAll(s, "firstpurchase", "first_purchase")
	return s
}
