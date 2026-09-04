// Package api 承载 HTTP 层：客户端决策 API、事件上报、管理 API。
// 路由统一在此注册，handler 按域拆分文件。
package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"runtime"
	"time"

	"adcenter/internal/budget"
	"adcenter/internal/config"
	"adcenter/internal/engine"
	"adcenter/internal/metrics"
	"adcenter/internal/store"
)

// generateAPIKey 生成 App API Key：原文 adc_{32hex}（仅创建时返回一次），
// 落库 sha256 哈希 + 展示前缀。
func generateAPIKey() (key, prefix, hash string, err error) {
	b := make([]byte, 16)
	if _, err = rand.Read(b); err != nil {
		return "", "", "", err
	}
	key = "adc_" + hex.EncodeToString(b)
	prefix = key[:12] + "…"
	sum := sha256.Sum256([]byte(key))
	return key, prefix, hex.EncodeToString(sum[:]), nil
}

// Server 聚合全部运行依赖（main 装配后注入）。
type Server struct {
	Cache       *config.Cache
	Engine      *engine.Engine
	Store       *store.Store
	Metrics     *metrics.Agg
	Budget      budget.Ctrl
	InternalKey string // 管理 API 内部密钥（BFF 共享）
	Log         *slog.Logger
	events      *eventWriter
}

// SetEventWriter 注入异步事件写入器（main 装配时调用）。
func (s *Server) SetEventWriter(w *eventWriter) { s.events = w }

// RunEventWriter 启动事件落库协程。
func (s *Server) RunEventWriter(ctx context.Context) { go s.events.run(ctx) }

// NewRouter 返回根路由（Go 1.26 方法路由）。
func (s *Server) NewRouter() *http.ServeMux {
	mux := http.NewServeMux()

	// 客户端 API（X-Api-Key 鉴权）
	mux.HandleFunc("POST /v1/ad/req", s.handleAdRequest)
	mux.HandleFunc("POST /v1/ad/event", s.handleAdEvent)

	// 管理 API（内部密钥 + RBAC）
	mux.HandleFunc("GET /v1/admin/advertisers", s.handleListAdvertisers)
	mux.HandleFunc("POST /v1/admin/advertisers", s.handleCreateAdvertiser)
	mux.HandleFunc("GET /v1/admin/advertisers/{id}", s.handleGetAdvertiser)
	mux.HandleFunc("PATCH /v1/admin/advertisers/{id}", s.handleUpdateAdvertiser)
	mux.HandleFunc("DELETE /v1/admin/advertisers/{id}", s.handleDeleteAdvertiser)
	mux.HandleFunc("GET /v1/admin/slots", s.handleListSlots)
	mux.HandleFunc("GET /v1/admin/apps", s.handleListApps)
	mux.HandleFunc("POST /v1/admin/apps", s.handleCreateApp)

	// 健康检查（Fly health check）
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	return mux
}

// handleHealthz 汇报进程存活与关键子系统状态。
type healthzResponse struct {
	Status        string    `json:"status"`
	UpSince       time.Time `json:"up_since"`
	Version       string    `json:"version"`
	GoVersion     string    `json:"go_version"`
	ConfigLoaded  bool      `json:"config_loaded"`
	ConfigApps    int       `json:"config_apps"`
	ConfigAdv     int       `json:"config_advertisers"`
	MetricsBuffer int       `json:"metrics_buffer"`
}

var startTime = time.Now().UTC()

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	snap := s.Cache.Snapshot()
	resp := healthzResponse{
		Status:        "ok",
		UpSince:       startTime,
		Version:       "dev",
		GoVersion:     runtime.Version(),
		ConfigLoaded:  snap != nil,
		MetricsBuffer: s.Metrics.Size(),
	}
	if snap != nil {
		resp.ConfigApps = len(snap.Apps)
		resp.ConfigAdv = len(snap.Advertisers)
	}
	writeJSON(w, http.StatusOK, resp)
}
