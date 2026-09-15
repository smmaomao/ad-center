package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"adcenter/internal/store"
)

// MetricsQueueStats 队列积压快照（来自事件队列 Backend.Stats）。
type MetricsQueueStats struct {
	Buffered int64 `json:"buffered"`
	Pending  int64 `json:"pending"`
	Dropped  int64 `json:"dropped"`
}

// MetricsSnapshot 看板 SSE 推送的一帧快照。
type MetricsSnapshot struct {
	Overview    *store.DashboardOverview   `json:"overview"`
	Advertisers []store.DashboardAdvertiser `json:"advertisers"`
	Slots       []store.DashboardSlot       `json:"slots"`
	Queue       MetricsQueueStats           `json:"queue"`
	ServerTime  int64                      `json:"server_time"`
}

// handleMetricsStream SSE 实时推送看板数据（阶段 3.1）。
//
//	GET /v1/admin/metrics/stream
//
// 鉴权复用 requireRole（只读）。每 2s 推一帧；客户端断开即退出。
func (s *Server) handleMetricsStream(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireRole(w, r, false, false); !ok {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // 关掉反代缓冲（nginx/fly proxy）
	w.WriteHeader(http.StatusOK)

	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()

	send := func() {
		snap, err := s.buildSnapshot(r.Context())
		if err != nil {
			s.Log.Warn("metrics snapshot failed", "err", err)
			return
		}
		b, err := json.Marshal(snap)
		if err != nil {
			return
		}
		fmt.Fprintf(w, "data: %s\n\n", b)
		flusher.Flush()
	}

	send() // 立即推首帧
	for {
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
			send()
		}
	}
}

// buildSnapshot 组装一帧看板数据。
func (s *Server) buildSnapshot(ctx context.Context) (*MetricsSnapshot, error) {
	ov, err := s.Store.DashboardOverview(ctx)
	if err != nil {
		return nil, err
	}
	adv, err := s.Store.DashboardAdvertisers(ctx)
	if err != nil {
		return nil, err
	}
	slots, err := s.Store.DashboardSlots(ctx)
	if err != nil {
		return nil, err
	}
	snap := &MetricsSnapshot{
		Overview:    ov,
		Advertisers: adv,
		Slots:       slots,
		ServerTime:  time.Now().UnixMilli(),
	}
	if s.Queue != nil {
		qs := s.Queue.Stats()
		snap.Queue = MetricsQueueStats{Buffered: qs.Buffered, Pending: qs.Pending, Dropped: qs.Dropped}
	}
	return snap, nil
}
