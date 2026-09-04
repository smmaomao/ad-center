package api

import (
	"net/http"
	"time"

	"adcenter/internal/engine"
	"adcenter/internal/store"
)

// adRequest POST /v1/ad/req 请求体。
type adRequest struct {
	Slot     string `json:"slot"`     // slot_key（客户端稳定标识）
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
	if req.DeviceID == "" || req.Slot == "" {
		writeError(w, http.StatusBadRequest, "slot and deviceId required")
		return
	}

	snap := s.Cache.Snapshot()
	slot := snap.SlotsByKey[req.Slot]
	if slot == nil {
		writeError(w, http.StatusNotFound, "unknown slot")
		return
	}
	if slot.AppID != app.ID {
		writeError(w, http.StatusForbidden, "slot not owned by app")
		return
	}

	now := time.Now()
	resp := s.Engine.Decide(snap, engine.Request{
		App: app, Slot: slot, DeviceID: req.DeviceID,
		Country: req.Country, Language: req.Language,
		Count: req.Count, Now: now,
	})

	// 记账（内存计数 + 异步事件），不在响应关键路径上等待落库
	var revenue float64
	var advID string
	if len(resp.Items) > 0 {
		advID = resp.Items[0].AdvertiserID
		for _, it := range resp.Items {
			revenue += it.BidPrice
		}
	}
	s.Metrics.Record(app.ID, slot.ID, advID, len(resp.Items), revenue, now)
	if len(resp.Items) > 0 {
		for _, it := range resp.Items {
			s.enqueueEvent(store.AdEvent{
				AppID: app.ID, SlotID: slot.ID, AdvertiserID: it.AdvertiserID,
				CreativeID: it.Creative.ID, DeviceID: req.DeviceID,
				Country: req.Country, EventType: "fill", Revenue: it.BidPrice,
			})
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// adEvent POST /v1/ad/event 请求体。
type adEvent struct {
	Event       string  `json:"event"` // impression / click / conversion
	Slot        string  `json:"slot"`
	DeviceID    string  `json:"deviceId"`
	AdvertiserID string `json:"advertiserId,omitempty"`
	CreativeID  string  `json:"creativeId,omitempty"`
	Revenue     float64 `json:"revenue,omitempty"`
}

// handleAdEvent 事件回执：记录 + 指标 + 转化时结转预算。
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
	case "impression", "click", "conversion":
	default:
		writeError(w, http.StatusBadRequest, "event must be impression/click/conversion")
		return
	}

	snap := s.Cache.Snapshot()
	slot := snap.SlotsByKey[req.Slot]
	if slot == nil {
		writeError(w, http.StatusNotFound, "unknown slot")
		return
	}

	now := time.Now()
	s.Metrics.RecordEvent(app.ID, slot.ID, req.AdvertiserID, req.Event, req.Revenue, now)
	s.enqueueEvent(store.AdEvent{
		AppID: app.ID, SlotID: slot.ID, AdvertiserID: req.AdvertiserID,
		CreativeID: req.CreativeID, DeviceID: req.DeviceID,
		EventType: req.Event, Revenue: req.Revenue,
	})
	// 转化回执：预算预扣结转（CPI 实际成本以回执为准，P0 用出价估计）
	if req.Event == "conversion" && req.AdvertiserID != "" {
		_ = s.Budget.Commit(req.AdvertiserID, req.Revenue)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
