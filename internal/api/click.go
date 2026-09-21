package api

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strings"
	"time"

	"adcenter/internal/store"
)

// adClickReq POST /v1/ad/click 请求体（客户端接口文档版）。
// 客户端在用户点击广告、跳转落地页之前调用，换取 click_id 拼进 click_url；
// 广告方（Adjust / H5 / PWA）把 click_id 当"用户标识"，后续事件经 S2S 带回。
type adClickReq struct {
	BidID      string `json:"bid_id"`      // 本次广告下发时返回的唯一下发交易ID（必填）
	AdAppID    string `json:"ad_app_id"`   // 应用ID（校验用，可空）
	CreativeID string `json:"creative_id"` // 素材ID（校验用，可空）
	UserID     string `json:"user_id"`     // 业务用户ID（归因/对账，可空）
	AdjustAdid string `json:"adjust_adid"` // 设备标识（可空）
	Timestamp  int64  `json:"timestamp"`   // 点击时间戳（秒，可空）
	Count      int    `json:"count"`       // 与下发一致的下发数量（统一参数，可空）
	IP         string `json:"ip"`          // 客户端IP（统一参数，可空）
	OS         string `json:"os"`          // 客户端系统（统一参数，可空）
}

// newClickID 128-bit 不可枚举随机串（base64url，≈22 字符），URL 友好。
func newClickID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// handleAdClick 生成唯一 click_id 并落点击登记表（ads_center.clicks），同时按计费
// 方式记一次有效点击（billing_mode=cpc 时扣费，cpa/cpm 广告主只记指标），返回
// click_id 供 App 拼进 click_url。归属上下文来自下发时的 bid 登记（防伪造归因）。
//
//	@Summary      点击埋点
//	@Description  用户点击广告跳转前调用，换取 click_id（及 jump_url）拼进落地页；服务端按计费方式记一次有效点击。
//	@Tags         客户端接口
//	@Accept       json
//	@Produce      json
//	@Param        X-Api-Key header string true "App API Key"
//	@Param        request body AdClickRequest true "点击埋点请求"
//	@Success      200 {object} AdClickResponse
//	@Router       /v1/ad/click [post]
func (s *Server) handleAdClick(w http.ResponseWriter, r *http.Request) {
	app := s.authenticateApp(w, r)
	if app == nil {
		return
	}
	var req adClickReq
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.BidID == "" {
		writeError(w, http.StatusBadRequest, "bid_id required")
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

	clickID, err := newClickID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "clickid generation failed")
		return
	}
	if err := s.Store.RegisterClick(r.Context(), clickID, store.ClickRecord{
		AppID: app.ID, AdvertiserID: bid.AdvertiserID,
		Style: bid.Style, DeviceID: bid.DeviceID, CreativeID: bid.CreativeID,
	}); err != nil {
		s.Log.Error("register click failed", "err", err)
		writeError(w, http.StatusInternalServerError, "register click failed")
		return
	}

	// 点击计费：billing_mode=cpc 时按 BiddingPrice 扣（跳转即一次有效点击）；
	// cpa/cpm 广告主此事件不扣，只记指标。金额以服务端配置为准。
	now := time.Now()
	charged := s.chargeClientEvent(app, bid.AdvertiserID, bid.CreativeID, bid.CampaignID, bid.Style, bid.DeviceID, "click", now)

	// 生成最终跳转地址：基于广告任务的落地页地址拼接 click_id，
	// 客户端拿到 jump_url 直接打开即可（无需自行替换 {CLICK_ID} 占位符）。
	jumpURL := ""
	if bid.LandingURL != "" {
		sep := "?"
		if strings.Contains(bid.LandingURL, "?") {
			sep = "&"
		}
		jumpURL = bid.LandingURL + sep + "clk=" + clickID
	}

	s.Log.Info("ad click", "click_id", clickID, "bid_id", req.BidID,
		"advertiser_id", bid.AdvertiserID, "charged", charged, "jump_url", jumpURL)
	resp := map[string]any{
		"code": 200, "msg": "success",
		"data": map[string]string{"click_id": clickID},
	}
	if jumpURL != "" {
		resp["data"].(map[string]string)["jump_url"] = jumpURL
	}
	writeJSON(w, http.StatusOK, resp)
}
