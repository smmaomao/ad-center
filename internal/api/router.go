// Package api 承载 HTTP 层：客户端决策 API、事件上报、管理 API。
// 路由统一在此注册，handler 按域拆分文件（decision.go / report.go / admin/）。
package api

import (
	"encoding/json"
	"net/http"
	"runtime"
	"time"
)

// NewRouter 返回根路由。
// 阶段 1/2 会陆续挂载：
//
//	POST /v1/ad/req          广告决策（客户端）
//	POST /v1/ad/event        事件上报（客户端）
//	/v1/admin/*              管理 API（Next.js BFF 转发）
//	/v1/admin/metrics/stream SSE 实时看板
func NewRouter() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealthz)
	return mux
}

// healthzResponse 汇报进程存活与关键子系统状态。
// 后续阶段接入决策缓存 / DB 连接 / 预算对账状态后，任一异常应返回 503，
// 供 Fly health check 拉起异常实例。
type healthzResponse struct {
	Status    string    `json:"status"`
	UpSince   time.Time `json:"up_since"`
	Version   string    `json:"version"`
	GoVersion string    `json:"go_version"`
}

var startTime = time.Now().UTC()

func runtimeVersion() string { return runtime.Version() }

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	resp := healthzResponse{
		Status:    "ok",
		UpSince:   startTime,
		Version:   "dev",
		GoVersion: runtimeVersion(),
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}
