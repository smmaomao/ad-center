package api

import (
	"net/http"
)

// handleMetricsStream 看板实时流（已暂停使用）。
//
//	GET /v1/admin/metrics/stream
//
// 临时停用 SSE 实时推送（避免连接与日志刷屏）。路由保留以便未来恢复，
// 当前仅返回 503 + 暂停说明，不建立长连接、不推送数据。
func (s *Server) handleMetricsStream(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireRole(w, r, false, false); !ok {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte(`{"paused":true,"message":"metrics stream temporarily disabled"}`))
}
