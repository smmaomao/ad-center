package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"adcenter/internal/config"
	"adcenter/internal/engine"
	"adcenter/internal/frequency"
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
// CampaignID 在下发时即由服务端记下，使频控计数在曝光处可直接取用，
// 无需再依赖配置快照反查 campaign——bid_id 缓存本身即闭环归因。
type BidContext struct {
	AppID        string
	AdvertiserID string
	CreativeID   string
	CampaignID   string // 素材归属任务（下发时快照，频控计数直接使用，免反查）
	Style        string
	DeviceID     string
	UserID       string
	AdjustAdid   string
	LandingURL   string // 广告任务落地页地址（点击时据此生成最终跳转地址 jump_url）
	ExpiresAt    time.Time
}

// BidStore 下发交易上下文登记表（bid_id → 上下文）。Redis 实现跨实例共享、
// 重启不丢；内存实现作为 Redis 不可用时的降级（与决策缓存同等级，见 SCALING.md）。
type BidStore interface {
	// Put 登记一次下发上下文，返回全局唯一 bid_id。
	Put(ctx context.Context, c *BidContext) string
	// Get 反查下发上下文；过期或不存在返回 false。
	Get(ctx context.Context, id string) (*BidContext, bool)
}

// NewMemBidStore 构造进程内下发上下文登记表（默认 30 分钟 TTL），用于无 Redis 环境。
func NewMemBidStore() BidStore {
	return &memBidStore{m: map[string]*BidContext{}, ttl: 30 * time.Minute}
}

// NewRedisBidStore 从 REDIS_URL 构造 Redis 下发上下文登记表（TTL 默认 30 分钟）。
// 返回具体类型以便调用方做 Ping / Close 探活与资源释放；其仍满足 BidStore 接口。
func NewRedisBidStore(redisURL string, ttl time.Duration) (*redisBidStore, error) {
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	return &redisBidStore{client: redis.NewClient(opt), prefix: "adcenter:bid:", ttl: ttl}, nil
}

// memBidStore 进程内 bid_id → 上下文 映射（带 TTL）。
type memBidStore struct {
	mu  sync.Mutex
	m   map[string]*BidContext
	ttl time.Duration
}

func (r *memBidStore) Put(ctx context.Context, c *BidContext) string {
	id, _ := newBidID()
	c.ExpiresAt = time.Now().Add(r.ttl)
	r.mu.Lock()
	r.m[id] = c
	r.mu.Unlock()
	return id
}

func (r *memBidStore) Get(ctx context.Context, id string) (*BidContext, bool) {
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

// redisBidStore Redis 版下发上下文登记表，跨实例共享且重启不丢。
// value 为 BidContext 的 JSON，键 TTL = ttl；过期由 Redis 承担。
type redisBidStore struct {
	client *redis.Client
	prefix string
	ttl    time.Duration
}

func (r *redisBidStore) Close() error                   { return r.client.Close() }
func (r *redisBidStore) Ping(ctx context.Context) error { return r.client.Ping(ctx).Err() }

func (r *redisBidStore) Put(ctx context.Context, c *BidContext) string {
	id, _ := newBidID()
	c.ExpiresAt = time.Now().Add(r.ttl)
	if b, err := json.Marshal(c); err == nil {
		_ = r.client.Set(ctx, r.prefix+id, b, r.ttl).Err()
	}
	return id
}

func (r *redisBidStore) Get(ctx context.Context, id string) (*BidContext, bool) {
	b, err := r.client.Get(ctx, r.prefix+id).Bytes()
	if err != nil {
		return nil, false // 不存在或 Redis 故障 → fail-open：当作无效 bid_id
	}
	var c BidContext
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, false
	}
	return &c, true
}

// 编译期接口实现检查。
var (
	_ BidStore = (*memBidStore)(nil)
	_ BidStore = (*redisBidStore)(nil)
)

// newBidID 生成全局唯一下发交易 ID：bid_{YYYYMMDD}{HHMMSS}_{8位随机}。
// 中间嵌入下发时刻的日期与时分秒，便于线上按 bid_id 直接定位时间排查；
// 末尾 8 位 base64url 随机串保证同一秒内唯一。
func newBidID() (string, error) {
	now := time.Now()
	randBytes := make([]byte, 6)
	if _, err := rand.Read(randBytes); err != nil {
		return "", err
	}
	return "bid_" + now.Format("20060102") + now.Format("150405") + "_" +
		base64.RawURLEncoding.EncodeToString(randBytes), nil
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
//	@Description  一次请求跨全部广告样式下发，按综合得分汇总取前 count 条；每条素材生成 bid_id（格式 bid_{日期}{时分秒}_{随机}，服务端缓存到 Redis），供后续曝光/点击/完播埋点闭环归因。
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
	allLoopable := true
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
		// 同时把 landing_url 与 campaign_id 记进 bid 上下文，点击/频控据此直接使用。
		clickURL := ""
		landingURL := ""
		campID, _ := snap.CreativeCampaign[cr.ID]
		if camp, ok2 := snap.Campaigns[campID]; ok2 && camp.LandingURL != "" {
			sep := "?"
			if strings.Contains(camp.LandingURL, "?") {
				sep = "&"
			}
			landingURL = camp.LandingURL
			clickURL = camp.LandingURL + sep + "clk={CLICK_ID}"
		}
		reqDur := 0
		if t.style == "rewarded_video" && cr.DurationMS > 0 {
			reqDur = cr.DurationMS / 1000
		}
		bid := s.Bids.Put(r.Context(), &BidContext{
			AppID: app.ID, AdvertiserID: it.AdvertiserID, CreativeID: cr.ID, CampaignID: campID,
			Style: t.style, DeviceID: deviceID, UserID: req.UserID, AdjustAdid: req.AdjustAdid,
			LandingURL: landingURL,
		})
		// loopable：客户端在缓存期内能否循环播放。当前由素材类型推导：
		// 视频素材允许循环（true），图片素材不循环（false）。
		// 该标记上提到 data 外层（整个广告列表是否可循环），当且仅当
		// 列表内所有素材均可循环时才为 true。
		loopable := cr.MediaType == "video"
		allLoopable = allLoopable && loopable
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
		})
		// fill 指标 + 事件（仅记日志/填充率，不计费）。
		s.Metrics.Record(app.ID, t.style, it.AdvertiserID, 1, 0, now)
		s.enqueueEvent(store.AdEvent{
			AppID: app.ID, Style: t.style, AdvertiserID: it.AdvertiserID,
			CreativeID: cr.ID, DeviceID: deviceID, EventType: "fill",
		})
	}

	if len(collected) == 0 {
		allLoopable = false
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"code": 200, "msg": "success",
		"data": map[string]any{
			"loopable": allLoopable,
			"ad_list":  adList,
		},
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
	bid, ok := s.Bids.Get(r.Context(), req.BidID)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid or expired bid_id")
		return
	}
	if req.CreativeID != "" && req.CreativeID != bid.CreativeID {
		writeError(w, http.StatusBadRequest, "creative_id mismatch")
		return
	}
	s.chargeClientEvent(app, bid.AdvertiserID, bid.CreativeID, bid.CampaignID, bid.Style, bid.DeviceID, "impression", time.Now())
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
	bid, ok := s.Bids.Get(r.Context(), req.BidID)
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
		"app_code":    app.ID,
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
func (s *Server) chargeClientEvent(app *config.App, advID, creativeID, campaignID, style, deviceID, event string, now time.Time) float64 {
	snap := s.Cache.Snapshot()
	var charged float64
	if adv := snap.Advertisers[advID]; adv != nil {
		if amt, ok := adv.BillingAmount(event); ok {
			if campID, ok := snap.CreativeCampaign[creativeID]; ok && campID != "" {
				// campaign 日预算闸 + 广告主总钱包闸：任一不足即不扣费
				if s.Budget.TryDeduct(campID, amt) && s.Budget.WalletDeduct(advID, amt) {
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
	// 任务级频控：仅在真实观看（impression）时累加计数。计数失败一律 fail-open
	// （不挡广告）——最坏是少限一次，不影响曝光。
	if event == "impression" {
		s.recordCampaignImpression(snap, app.ID, creativeID, campaignID, deviceID, now)
	}
	return charged
}

// recordCampaignImpression 在 impression 时累加"设备×任务"频控计数：
// campaignID 优先取下发时登记的 bid 上下文（Redis 缓存，免反查快照），
// 缺失时回退到配置快照 CreativeCampaign 反查，兼容历史路径。
// 窗口定义复用与决策期 Check 同款的 engine.CampaignFreqWindows，
// 保证"只读检查"与"真实观看计数"一致。
func (s *Server) recordCampaignImpression(snap *config.Snapshot, appID, creativeID, campaignID, deviceID string, now time.Time) {
	if s.Freq == nil {
		return
	}
	if campaignID == "" {
		var ok bool
		campaignID, ok = snap.CreativeCampaign[creativeID]
		if !ok || campaignID == "" {
			return
		}
	}
	camp := snap.Campaigns[campaignID]
	if camp == nil {
		return
	}
	if ws := engine.CampaignFreqWindows(camp); len(ws) > 0 {
		s.Freq.Record(appID, deviceID, creativeID, campaignID, frequency.AdvPolicy{Windows: ws}, now)
	}
}
