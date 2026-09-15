package api

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"adcenter/internal/config"
	"adcenter/internal/engine"
	"adcenter/internal/store"
)

// ============================================================
// 客户端接口（文档 docs/客户端接口.md 契约）：
//
//	POST /v1/ad/list          批量获取广告（接口 1）
//	POST /v1/ad/impression    曝光埋点（接口 2）
//	POST /v1/ad/click         点击埋点，返回 click_id（接口 3）
//	POST /v1/ad/video-complete 激励视频播放完毕（接口 4）
//
// 与内部既有 /v1/ad/req、/v1/ad/event 不同：用 bid_id 串联
// 「下发 → 曝光 → 点击 → 完播」，click_url 携带 {CLICK_ID} 占位符。
// ============================================================

// BidContext 一次下发（bid）的归因上下文：下发时服务端快照登记，
// 后续曝光/点击/完播凭 bid_id 还原，避免客户端自报归属参数（防伪造）。
type BidContext struct {
	AppID        string
	AdvertiserID string
	CreativeID   string
	Style        string
	DeviceID     string
	UserID       string
	AdjustAdid   string
	LandingURL   string // 广告任务落地页地址（点击时据此生成最终跳转地址 jump_url）
	ExpiresAt    time.Time
}

// BidRegistry 进程内 bid_id → 上下文 映射（带 TTL）。本地/单实例够用；
// 多实例部署需外移 Redis（与决策缓存同等级，见 SCALING.md）。
type BidRegistry struct {
	mu  sync.Mutex
	m   map[string]*BidContext
	ttl time.Duration
}

// NewBidRegistry 构造下发上下文登记表（默认 30 分钟 TTL）。
func NewBidRegistry() *BidRegistry {
	return &BidRegistry{m: map[string]*BidContext{}, ttl: 30 * time.Minute}
}

// newBidID 生成全局唯一下发交易 ID（"bid_" + 96-bit base64url）。
func newBidID() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "bid_" + base64.RawURLEncoding.EncodeToString(b), nil
}

// Put 登记一次下发上下文，返回全局唯一 bid_id。
func (r *BidRegistry) Put(c *BidContext) string {
	id, _ := newBidID()
	c.ExpiresAt = time.Now().Add(r.ttl)
	r.mu.Lock()
	r.m[id] = c
	r.mu.Unlock()
	return id
}

// Get 反查下发上下文；过期或不存在返回 false。
func (r *BidRegistry) Get(id string) (*BidContext, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.m[id]
	if !ok {
		return nil, false
	}
	if time.Now().After(c.ExpiresAt) {
		delete(r.m, id)
		return nil, false
	}
	return c, true
}

// ---- 文档字段映射 ----

func styleToDoc(s string) string {
	switch s {
	case "rewarded_video":
		return "REWARDED_VIDEO"
	case "interstitial":
		return "INTERSTITIAL"
	case "splash":
		return "SPLASH"
	case "banner":
		return "BANNER"
	case "feed":
		return "FEED"
	default:
		return strings.ToUpper(s)
	}
}

func mediaToDoc(m string) string {
	switch m {
	case "video":
		return "VIDEO"
	case "image":
		return "IMAGE"
	case "html":
		return "HTML"
	default:
		return strings.ToUpper(m)
	}
}

func sceneForStyle(s string) []string {
	switch s {
	case "splash":
		return []string{"APP_LAUNCH"}
	case "rewarded_video":
		return []string{"REWARD"}
	case "interstitial":
		return []string{"LEVEL_CLEAR", "PAUSE"}
	case "feed":
		return []string{"FEED"}
	case "banner":
		return []string{"MAIN_BOTTOM"}
	default:
		return []string{}
	}
}

// handleAdList POST /v1/ad/list 批量获取广告（文档接口 1）。
//
// 文档请求不带 style：跨全部样式下发，按得分汇总取前 count 条。
// 每条素材生成一个 bid_id，后续埋点必须回传该 bid_id 以闭环归因。
//
//	@Summary      批量获取广告
//	@Description  一次请求跨全部广告样式下发，按综合得分汇总取前 count 条；每条素材生成 bid_id，供后续曝光/点击/完播埋点闭环归因。
//	@Tags         客户端接口
//	@Accept       json
//	@Produce      json
//	@Param        X-Api-Key header string true "App API Key"
//	@Param        request body AdListRequest true "批量获取广告请求（body 可空，参数亦可走 query）"
//	@Success      200 {object} AdListResponse
//	@Router       /v1/ad/list [post]
func (s *Server) handleAdList(w http.ResponseWriter, r *http.Request) {
	app := s.authenticateApp(w, r)
	if app == nil {
		return
	}
	var req struct {
		AdAppID    string `json:"ad_app_id"`
		Count      int    `json:"count"`
		UserID     string `json:"user_id"`
		AdjustAdid string `json:"adjust_adid"`
		OS         string `json:"os"`
		IP         string `json:"ip"`
	}
	// 列表接口 body 可选（参数走 query）；空 body 视为无扩展参数
	if r.Body != nil {
		if raw, _ := io.ReadAll(r.Body); len(bytes.TrimSpace(raw)) > 0 {
			if err := json.Unmarshal(raw, &req); err != nil {
				writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
				return
			}
		}
	}
	if req.AdAppID != "" && req.AdAppID != app.ID {
		writeError(w, http.StatusBadRequest, "ad_app_id mismatch")
		return
	}
	count := req.Count
	if count < 1 {
		count = 5
	}
	if count > engine.MaxCount {
		count = engine.MaxCount
	}
	// 频控/反作弊主体：优先设备标识，其次用户ID，再回退 IP / App。
	deviceID := req.AdjustAdid
	if deviceID == "" {
		deviceID = req.UserID
	}
	if deviceID == "" {
		deviceID = req.IP
	}
	if deviceID == "" {
		deviceID = app.ID
	}

	snap := s.Cache.Snapshot()
	now := time.Now()

	// 跨全部样式决策，按素材+样式去重，汇总后按得分取前 count 条。
	type tagged struct {
		item  engine.Item
		style string
	}
	collected := make([]tagged, 0)
	seen := map[string]bool{}
	for _, style := range engine.Styles {
		resp := s.Engine.Decide(snap, engine.Request{
			App: app, Style: style, DeviceID: deviceID, Count: count, Now: now,
		})
		for _, it := range resp.Items {
			if it.Creative == nil {
				continue
			}
			key := it.Creative.ID + "|" + style
			if seen[key] {
				continue
			}
			seen[key] = true
			collected = append(collected, tagged{it, style})
		}
	}
	sort.SliceStable(collected, func(i, j int) bool {
		return collected[i].item.Score > collected[j].item.Score
	})
	if len(collected) > count {
		collected = collected[:count]
	}

	adList := make([]map[string]any, 0, len(collected))
	for _, t := range collected {
		it := t.item
		cr := it.Creative
		materialURL := it.MediaURL
		if materialURL == "" && s.Storage != nil {
			materialURL = s.Storage.PresignGET(cr.StoragePath, s.Storage.DefaultExpiry())
		}
		if materialURL == "" {
			materialURL = cr.StoragePath
		}
		// click_url / landing_url：来自广告任务 landing_url，{CLICK_ID} 占位符由客户端替换；
		// 同时把 landing_url 记进 bid 上下文，点击接口据此生成最终跳转地址 jump_url。
		clickURL := ""
		landingURL := ""
		if campID, ok := snap.CreativeCampaign[cr.ID]; ok {
			if camp, ok2 := snap.Campaigns[campID]; ok2 && camp.LandingURL != "" {
				sep := "?"
				if strings.Contains(camp.LandingURL, "?") {
					sep = "&"
				}
				landingURL = camp.LandingURL
				clickURL = camp.LandingURL + sep + "clk={CLICK_ID}"
			}
		}
		reqDur := 0
		if t.style == "rewarded_video" && cr.DurationMS > 0 {
			reqDur = cr.DurationMS / 1000
		}
		bid := s.Bids.Put(&BidContext{
			AppID: app.ID, AdvertiserID: it.AdvertiserID, CreativeID: cr.ID,
			Style: t.style, DeviceID: deviceID, UserID: req.UserID, AdjustAdid: req.AdjustAdid,
			LandingURL: landingURL,
		})
		// loopable：客户端在缓存期内能否循环播放。当前由素材类型推导：
		// 视频素材允许循环（true），图片素材不循环（false）。
		loopable := cr.MediaType == "video"
		adList = append(adList, map[string]any{
			"bid_id":            bid,
			"creative_id":       cr.ID,
			"ad_style":          styleToDoc(t.style),
			"material_type":     mediaToDoc(cr.MediaType),
			"material_url":      materialURL,
			"width":             cr.Width,
			"height":            cr.Height,
			"target_scene":      sceneForStyle(t.style),
			"required_duration": reqDur,
			"click_url":         clickURL,
			"loopable":          loopable,
		})
		// fill 指标 + 事件（仅记日志/填充率，不计费）。
		s.Metrics.Record(app.ID, t.style, it.AdvertiserID, 1, 0, now)
		s.enqueueEvent(store.AdEvent{
			AppID: app.ID, Style: t.style, AdvertiserID: it.AdvertiserID,
			CreativeID: cr.ID, DeviceID: deviceID, EventType: "fill",
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"code": 200, "msg": "success",
		"data": map[string]any{"ad_list": adList},
	})
}

// handleAdImpression POST /v1/ad/impression 曝光埋点（文档接口 2）。
//
//	@Summary      曝光埋点
//	@Description  素材成功渲染并被用户看到时上报，触发 CPM 扣费与展现统计。
//	@Tags         客户端接口
//	@Accept       json
//	@Produce      json
//	@Param        X-Api-Key header string true "App API Key"
//	@Param        request body AdImpressionRequest true "曝光埋点请求"
//	@Success      200 {object} ClientSimpleResponse
//	@Router       /v1/ad/impression [post]
func (s *Server) handleAdImpression(w http.ResponseWriter, r *http.Request) {
	app := s.authenticateApp(w, r)
	if app == nil {
		return
	}
	var req struct {
		BidID      string `json:"bid_id"`
		AdAppID    string `json:"ad_app_id"`
		CreativeID string `json:"creative_id"`
		Timestamp  int64  `json:"timestamp"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	bid, ok := s.Bids.Get(req.BidID)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid or expired bid_id")
		return
	}
	if req.CreativeID != "" && req.CreativeID != bid.CreativeID {
		writeError(w, http.StatusBadRequest, "creative_id mismatch")
		return
	}
	s.chargeClientEvent(app, bid.AdvertiserID, bid.CreativeID, bid.Style, bid.DeviceID, "impression", time.Now())
	writeJSON(w, http.StatusOK, map[string]any{"code": 200, "msg": "success"})
}

// handleAdVideoComplete POST /v1/ad/video-complete 激励视频播放完毕（文档接口 4）。
// 服务端校验后异步触发 S2S 回调，通知业务后端给 user_id 发奖励。
//
//	@Summary      视频播放完毕
//	@Description  仅 REWARDED_VIDEO 样式：观看达到 required_duration 秒或自然播完时上报，触发 S2S 回调发奖。
//	@Tags         客户端接口
//	@Accept       json
//	@Produce      json
//	@Param        X-Api-Key header string true "App API Key"
//	@Param        request body AdVideoCompleteRequest true "视频播放完毕请求"
//	@Success      200 {object} ClientSimpleResponse
//	@Router       /v1/ad/video-complete [post]
func (s *Server) handleAdVideoComplete(w http.ResponseWriter, r *http.Request) {
	app := s.authenticateApp(w, r)
	if app == nil {
		return
	}
	var req struct {
		BidID      string `json:"bid_id"`
		AdAppID    string `json:"ad_app_id"`
		CreativeID string `json:"creative_id"`
		UserID     string `json:"user_id"`
		Timestamp  int64  `json:"timestamp"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.UserID == "" {
		writeError(w, http.StatusBadRequest, "user_id required")
		return
	}
	bid, ok := s.Bids.Get(req.BidID)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid or expired bid_id")
		return
	}
	if req.CreativeID != "" && req.CreativeID != bid.CreativeID {
		writeError(w, http.StatusBadRequest, "creative_id mismatch")
		return
	}
	s.fireRewardCallback(app, bid, req.BidID, req.UserID, req.Timestamp)
	writeJSON(w, http.StatusOK, map[string]any{"code": 200, "msg": "success"})
}

// fireRewardCallback 异步向 App 的业务后端回调地址发送激励视频完播通知。
// callback_url 未配置时仅记日志（不发网络请求）。失败只告警，不影响本次响应。
func (s *Server) fireRewardCallback(app *config.App, bid *BidContext, bidID, userID string, ts int64) {
	if app.CallbackURL == "" {
		s.Log.Info("video complete: no callback_url configured, skip S2S",
			"app_code", app.ID, "user_id", userID, "bid_id", bidID)
		return
	}
	payload := map[string]any{
		"event":       "rewarded_video_complete",
		"user_id":     userID,
		"bid_id":      bidID,
		"creative_id": bid.CreativeID,
		"app_code":      app.ID,
		"style":       bid.Style,
		"timestamp":   ts,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		s.Log.Error("marshal reward callback failed", "err", err)
		return
	}
	url := app.CallbackURL
	go func() {
		resp, err := http.Post(url, "application/json", bytes.NewReader(b))
		if err != nil {
			s.Log.Warn("reward callback failed", "url", url, "err", err)
			return
		}
		resp.Body.Close()
		s.Log.Info("reward callback sent", "url", url, "user_id", userID, "bid_id", bidID)
	}()
}

// chargeClientEvent 按计费方式确认扣费并记指标/事件（供客户端埋点复用）。
// 金额以服务端配置为准，不接受客户端上报（防伪造刷量）。返回实际扣费金额。
func (s *Server) chargeClientEvent(app *config.App, advID, creativeID, style, deviceID, event string, now time.Time) float64 {
	snap := s.Cache.Snapshot()
	var charged float64
	if adv := snap.Advertisers[advID]; adv != nil {
		if amt, ok := adv.BillingAmount(event); ok {
			if campID, ok := snap.CreativeCampaign[creativeID]; ok && campID != "" {
				if s.Budget.TryDeduct(campID, amt) {
					charged = amt
				}
			}
		}
	}
	s.Metrics.RecordEvent(app.ID, style, advID, event, charged, now)
	s.enqueueEvent(store.AdEvent{
		AppID: app.ID, Style: style, AdvertiserID: advID,
		CreativeID: creativeID, DeviceID: deviceID,
		EventType: event, Revenue: charged,
	})
	if event == "impression" && s.Fatigue != nil {
		if fc := snap.FatigueConfig(); fc.Enabled {
			s.Fatigue.Record(deviceID, creativeID, fc)
		}
	}
	return charged
}
