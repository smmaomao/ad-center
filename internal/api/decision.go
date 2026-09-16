package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"adcenter/internal/config"
	"adcenter/internal/engine"
	"adcenter/internal/store"
)

// adRequest POST /v1/ad/req 请求体。
type adRequest struct {
	Style    string `json:"style"`    // 展现样式：splash / rewarded_video / interstitial / feed / banner
	DeviceID string `json:"deviceId"` // 设备 ID（频控主体）
	Country  string `json:"country,omitempty"`
	Language string `json:"language,omitempty"`
	Count    int    `json:"count,omitempty"` // 批量条数（默认 1，上限 20）
}

// handleAdRequest 广告决策：全内存路径（鉴权查快照 + 引擎纯计算），
// 目标 <10ms（不含网络）。
func (s *Server) handleAdRequest(w http.ResponseWriter, r *http.Request) {
	app := s.authenticateApp(w, r)
	if app == nil {
		return
	}
	var req adRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.DeviceID == "" || req.Style == "" {
		writeError(w, http.StatusBadRequest, "style and deviceId required")
		return
	}
	if !engine.ValidStyle(req.Style) {
		writeError(w, http.StatusBadRequest, "unknown style: "+req.Style)
		return
	}

	snap := s.Cache.Snapshot()

	now := time.Now()
	resp := s.resolveDecision(r, snap, app, req, now)

	// 素材下载地址：配 CDN 自定义域时为纯净公开 URL；未配则 R2 预签名 GET（直连私有桶兜底）/ html 直链。
	// 纯内存 HMAC（微秒级），不触碰决策延迟预算；未配置 R2 时省略字段。
	// 命中缓存时也需重算（预签名短时效，且 expire_at 已在 resolveDecision 用
	// 当前时间重算）。
	if s.Storage != nil {
		for i := range resp.Items {
			it := &resp.Items[i]
			if it.Creative == nil {
				continue
			}
			if it.Creative.MediaType == "html" {
				it.MediaURL = it.Creative.StoragePath // html 存完整 URL，不签名
			} else {
				it.MediaURL = s.Storage.PresignGET(it.Creative.StoragePath, s.Storage.DefaultExpiry())
				it.CreativeHash = contentHash(it.Creative.StoragePath)
			}
		}
	}

	// 记账：fill 只是"下发成功"日志（填充率指标），不产生收入——
	// 收入/扣费只发生在真实计费事件到达时（impression/click/S2S 转化），
	// 见 handleAdEvent 与 handleS2SEvent。revenue 一律 0。
	var advID string
	if len(resp.Items) > 0 {
		advID = resp.Items[0].AdvertiserID
	}
	s.Metrics.Record(app.ID, req.Style, advID, len(resp.Items), 0, now)
	if len(resp.Items) > 0 {
		for _, it := range resp.Items {
			s.enqueueEvent(store.AdEvent{
				AppID: app.ID, Style: req.Style, AdvertiserID: it.AdvertiserID,
				CreativeID: it.Creative.ID, DeviceID: req.DeviceID,
				Country: req.Country, EventType: "fill",
			})
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// resolveDecision 决策 + 结果缓存（Redis）。
//
// 命中缓存时复用已算好的候选（评分/排序/频控/保量等重计算被跳过，预算/频控
// 在缓存窗口内少量超发，符合"近似即可、预算可扣负"的取舍）；但 expire_at 这类
// 时间敏感字段在返回前用当前时间与广告主配置重算，保证客户端过期判断准确。
func (s *Server) resolveDecision(r *http.Request, snap *config.Snapshot, app *config.App, req adRequest, now time.Time) engine.Response {
	cfg := snap.DecisionCacheConfig()
	if s.DecisionCache != nil && cfg.Enabled {
		key := decisionCacheKey(app.ID, req.Style, req)
		if b, hit, err := s.DecisionCache.Get(r.Context(), key); err == nil && hit {
			var cached engine.Response
			if json.Unmarshal(b, &cached) == nil {
				recomputeExpireAt(snap, cached.Items, now)
				return cached
			}
		}
		// miss：实时计算并回填缓存
		resp := s.Engine.Decide(snap, engine.Request{
			App: app, Style: req.Style, DeviceID: req.DeviceID,
			Country: req.Country, Language: req.Language,
			Count: req.Count, Now: now,
		})
		if b, err := json.Marshal(resp); err == nil {
			_ = s.DecisionCache.Set(r.Context(), key, b, time.Duration(cfg.TTLSeconds)*time.Second)
		}
		return resp
	}
	return s.Engine.Decide(snap, engine.Request{
		App: app, Style: req.Style, DeviceID: req.DeviceID,
		Country: req.Country, Language: req.Language,
		Count: req.Count, Now: now,
	})
}

// contentHashRe 从对象 key（creatives/{sha256}.ext）末尾提取内容 SHA-256。
var contentHashRe = regexp.MustCompile(`([0-9a-f]{64})\.[a-z0-9]+$`)

// contentHash 提取素材内容哈希；非内容寻址命名（如历史 UUID）或 html 外链返回空。
func contentHash(storagePath string) string {
	if m := contentHashRe.FindStringSubmatch(storagePath); m != nil {
		return m[1]
	}
	return ""
}

// recomputeExpireAt 缓存命中后，按当前时间与各广告主 deliver_ttl 重算 expire_at。
func recomputeExpireAt(snap *config.Snapshot, items []engine.Item, now time.Time) {
	for i := range items {
		it := &items[i]
		ttl := config.DefaultDeliverTTLMinutes
		if it.Creative != nil {
			if campID, ok := snap.CreativeCampaign[it.Creative.ID]; ok {
				if camp := snap.Campaigns[campID]; camp != nil {
					ttl = camp.DeliverTTL()
				}
			}
		}
		it.ExpireAt = now.Add(time.Duration(ttl) * time.Minute).Unix()
	}
}

// decisionCacheKey 决策缓存键：由所有影响决策的输入决定（app/style/device/
// country/language/count）。Now 不入键——同一窗口内相同输入返回同一结果。
// Count 按引擎规则归一化（<1→1，>MaxCount→MaxCount）以对齐引擎实际行为。
func decisionCacheKey(appID, style string, req adRequest) string {
	c := req.Count
	if c < 1 {
		c = 1
	}
	if c > engine.MaxCount {
		c = engine.MaxCount
	}
	h := sha256.New()
	h.Write([]byte(appID))
	h.Write([]byte{0})
	h.Write([]byte(style))
	h.Write([]byte{0})
	h.Write([]byte(req.DeviceID))
	h.Write([]byte{0})
	h.Write([]byte(req.Country))
	h.Write([]byte{0})
	h.Write([]byte(req.Language))
	h.Write([]byte{0})
	h.Write([]byte(strconv.Itoa(c)))
	return "decision:" + hex.EncodeToString(h.Sum(nil))
}

// adEvent POST /v1/ad/event 请求体。
// 仅 impression / click（转化走 S2S）。金额由服务端按计费配置计算，
// 不接受客户端上报 revenue（防伪造刷量）。
type adEvent struct {
	Event        string `json:"event"` // impression / click
	Style        string `json:"style"`
	DeviceID     string `json:"deviceId"`
	AdvertiserID string `json:"advertiserId,omitempty"`
	CreativeID   string `json:"creativeId,omitempty"`
}

// handleAdEvent 客户端事件回执：记录 + 指标 + 按计费方式扣费。
//
// 客户端只回执两类事件：impression（真播了）/ click（被点了）。
// 转化（安装/激活/注册/首充）由归因方（Adjust 等）或广告主服务器通过 S2S
// postback 通知，走 GET /v1/s2s/event——客户端自报转化可被刷量（伪造 install
// 虚增广告主消耗），且归因方也无法持有 App 的 API Key。
//
// 扣费锚点（migration 000008，由广告主 billing_mode 决定）：
//
//	cpm → impression 到达即扣 BiddingPrice/1000（真实曝光才花钱）
//	cpc → click 到达即扣 BiddingPrice
//	cpa → 这两个事件只是过程指标，不扣费（钱在 S2S 转化到达时扣）
//
// 金额以服务端配置为准，不接受客户端上报的 revenue（防伪造）。
func (s *Server) handleAdEvent(w http.ResponseWriter, r *http.Request) {
	app := s.authenticateApp(w, r)
	if app == nil {
		return
	}
	var req adEvent
	if !decodeJSON(w, r, &req) {
		return
	}
	switch req.Event {
	case "impression", "click":
	default:
		writeError(w, http.StatusBadRequest,
			"event must be impression/click (conversion goes through S2S postback)")
		return
	}

	snap := s.Cache.Snapshot()

	now := time.Now()
	// 按计费方式确认扣费；余额不足时事件照记（真实曝光/点击已发生），本次不扣——
	// 接受少量在途超发，req 路径的只读闸会在 spent >= budget 后停止下发。
	var charged float64
	if adv := snap.Advertisers[req.AdvertiserID]; adv != nil {
		if amt, ok := adv.BillingAmount(req.Event); ok {
			// 预算闸按 campaign 各自控制：创意归属的 campaign 为扣费单元；
			// 未挂到任何 campaign 的素材不扣费（也不参与预算封顶）。
			if campID, ok := snap.CreativeCampaign[req.CreativeID]; ok && campID != "" {
				if s.Budget.TryDeduct(campID, amt) {
					charged = amt
				}
			}
		}
	}
	s.Metrics.RecordEvent(app.ID, req.Style, req.AdvertiserID, req.Event, charged, now)
	s.enqueueEvent(store.AdEvent{
		AppID: app.ID, Style: req.Style, AdvertiserID: req.AdvertiserID,
		CreativeID: req.CreativeID, DeviceID: req.DeviceID,
		EventType: req.Event, Revenue: charged, // revenue = 实际扣费（服务端算），不是客户端上报
	})

	// 任务级频控：仅在真实观看（impression）时累加计数（详见 client.go 的
	// recordCampaignImpression，二者共用同一逻辑，避免重复实现）。
	if req.Event == "impression" {
		s.recordCampaignImpression(snap, app.ID, req.CreativeID, req.DeviceID, now)
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
