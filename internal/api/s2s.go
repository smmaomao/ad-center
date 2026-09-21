package api

import (
	"net/http"
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

// handleS2SEvent 归因方 S2S 转化回调（GET，用户决定暂不加独立密钥）。
//
// 客户端不应（也无法）上报转化：安装/激活/注册/首充/充值只能由归因平台
// （Adjust/AppsFlyer…）或广告主服务器确认后通知。此回调与 Adjust 的转化
// 回调参数对齐，参数由归因方模板宏展开回传：
//
//	clickid     必填。投放时为一次点击生成的唯一 ID；Adjust 点击事件存下后，
//	             转化回调用 {clickid} 宏原样带回。服务端凭它反查点击上下文
//	             （ClickResolver）。具体透传方式（素材跳转链接参数等）待定。
//	event_name  必填。install / activate / register / first_purchase / purchase
//	pixelId     归因方/中介（berealads）的转化像素/任务标识。落库到 ad_events.pixel_id
//	             便于"按中介侧任务标记"查看与统计；不参与归属（归属只信 clickid 反查）。
//	             建议 berealads 侧配置为广告任务 id，与服务端 campaign_id 对照。
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
	q := r.URL.Query()
	clickID := q.Get("clickid")
	eventName := q.Get("event_name")
	pixelID := q.Get("pixelId")
	currency := q.Get("currency")
	valueStr := q.Get("value")

	if clickID == "" {
		writeError(w, http.StatusBadRequest, "clickid required")
		return
	}
	if !slices.Contains(config.ConversionEvents, eventName) {
		writeError(w, http.StatusBadRequest,
			"event_name must be one of "+strings.Join(config.ConversionEvents, "/"))
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
