// Package api 承载 HTTP 层：客户端决策 API、事件上报、管理 API。
// 路由统一在此注册，handler 按域拆分文件。
package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"runtime"
	"time"

	httpSwagger "github.com/swaggo/http-swagger"
	"adcenter/internal/budget"
	"adcenter/internal/cache"
	"adcenter/internal/config"
	"adcenter/internal/engine"
	"adcenter/internal/fatigue"
	"adcenter/internal/metrics"
	"adcenter/internal/queue"
	"adcenter/internal/storage"
	"adcenter/internal/store"
)

// generateAPIKey 生成 App API Key：原文 adc_{32hex}（仅创建时返回一次），落库 sha256 哈希。
func generateAPIKey() (key, hash string, err error) {
	b := make([]byte, 16)
	if _, err = rand.Read(b); err != nil {
		return "", "", err
	}
	key = "adc_" + hex.EncodeToString(b)
	sum := sha256.Sum256([]byte(key))
	return key, hex.EncodeToString(sum[:]), nil
}

// Decider 决策引擎接口（*engine.Engine 实现）。抽出来便于缓存逻辑单测注入桩。
type Decider interface {
	Decide(snap *config.Snapshot, req engine.Request) engine.Response
}

// Server 聚合全部运行依赖（main 装配后注入）。
type Server struct {
	Cache         *config.Cache
	Engine        Decider
	Store         *store.Store
	Metrics       *metrics.Agg
	Budget        budget.Ctrl
	InternalKey   string // 管理 API 内部密钥（BFF 共享）
	Log           *slog.Logger
	Storage       *storage.Signer     // R2 预签名（nil = 未配置，下发不含 media_url）
	Clicks        ClickResolver       // clickid 归因反查（S2S 转化用）；nil = 未接入，转化只确认不扣费
	DecisionCache cache.DecisionCache // 决策结果缓存（Redis；nil/Noop = 实时计算）
	Queue         queue.Backend       // 事件队列（M2：memory / redis streams）；nil = 丢弃事件
	Fatigue       fatigue.Store       // 全局疲劳度（用户×素材）；nil = 不启用
	Bids          *BidRegistry        // 下发交易上下文登记表（客户端接口 bid_id → 上下文）
}

// NewRouter 返回根路由（Go 1.26 方法路由），外层包了请求计时日志中间件。
func (s *Server) NewRouter() http.Handler {
	mux := http.NewServeMux()

	// 客户端 API（X-Api-Key 鉴权）
	mux.HandleFunc("POST /v1/ad/req", s.handleAdRequest)
	mux.HandleFunc("POST /v1/ad/event", s.handleAdEvent)
	// 客户端接口（docs/客户端接口.md 契约）：bid_id 串联下发→曝光→点击→完播
	mux.HandleFunc("POST /v1/ad/list", s.handleAdList)
	mux.HandleFunc("POST /v1/ad/impression", s.handleAdImpression)
	mux.HandleFunc("POST /v1/ad/click", s.handleAdClick) // 生成 clickid + 落登记表（跳转前调用）
	mux.HandleFunc("POST /v1/ad/video-complete", s.handleAdVideoComplete)

	// 归因方 S2S 转化回调（无 App Key，暂时无独立密钥；GET 适配 Adjust postback）
	mux.HandleFunc("GET /v1/s2s/event", s.handleS2SEvent)

	// 管理 API（内部密钥 + RBAC）
	mux.HandleFunc("GET /v1/admin/advertisers", s.handleListAdvertisers)
	mux.HandleFunc("POST /v1/admin/advertisers", s.handleCreateAdvertiser)
	mux.HandleFunc("GET /v1/admin/advertisers/{id}", s.handleGetAdvertiser)
	mux.HandleFunc("PATCH /v1/admin/advertisers/{id}", s.handleUpdateAdvertiser)
	mux.HandleFunc("DELETE /v1/admin/advertisers/{id}", s.handleDeleteAdvertiser)
	mux.HandleFunc("GET /v1/admin/advertisers/{id}/rollup", s.handleRollupAdvertiserKPIs)
	mux.HandleFunc("GET /v1/admin/slots", s.handleListSlots)
	mux.HandleFunc("POST /v1/admin/slots", s.handleCreateSlot)
	mux.HandleFunc("GET /v1/admin/slots/{id}", s.handleGetSlot)
	mux.HandleFunc("PATCH /v1/admin/slots/{id}", s.handleUpdateSlot)
	mux.HandleFunc("DELETE /v1/admin/slots/{id}", s.handleDeleteSlot)
	mux.HandleFunc("GET /v1/admin/apps", s.handleListApps)
	mux.HandleFunc("POST /v1/admin/apps", s.handleCreateApp)
	mux.HandleFunc("PATCH /v1/admin/apps/{id}", s.handleUpdateApp)
	mux.HandleFunc("DELETE /v1/admin/apps/{id}", s.handleDeleteApp)
	mux.HandleFunc("POST /v1/admin/apps/{id}/secret", s.handleResetAppSecret)
	mux.HandleFunc("POST /v1/admin/apps/{id}/key", s.handleResetAppKey)
	mux.HandleFunc("GET /v1/admin/creatives", s.handleListCreatives)
	mux.HandleFunc("POST /v1/admin/creatives", s.handleCreateCreative)
	mux.HandleFunc("PATCH /v1/admin/creatives/{id}", s.handleUpdateCreative)
	mux.HandleFunc("DELETE /v1/admin/creatives/{id}", s.handleDeleteCreative)
	mux.HandleFunc("GET /v1/admin/creatives/{id}/play-url", s.handleCreativePlayURL)

	// 广告任务（Campaign）：广告主下的出价 / KPI / 单价 / 频控 + 关联素材
	mux.HandleFunc("GET /v1/admin/campaigns", s.handleListCampaigns)
	mux.HandleFunc("POST /v1/admin/campaigns", s.handleCreateCampaign)
	mux.HandleFunc("GET /v1/admin/campaigns/{id}", s.handleGetCampaign)
	mux.HandleFunc("PATCH /v1/admin/campaigns/{id}", s.handleUpdateCampaign)
	mux.HandleFunc("DELETE /v1/admin/campaigns/{id}", s.handleDeleteCampaign)

	// 产品（Product）：广告主下的产品归集（多产品广告主）
	mux.HandleFunc("GET /v1/admin/products", s.handleListProducts)
	mux.HandleFunc("POST /v1/admin/products", s.handleCreateProduct)
	mux.HandleFunc("GET /v1/admin/products/{id}", s.handleGetProduct)
	mux.HandleFunc("PATCH /v1/admin/products/{id}", s.handleUpdateProduct)
	mux.HandleFunc("DELETE /v1/admin/products/{id}", s.handleDeleteProduct)
	mux.HandleFunc("GET /v1/admin/products/{id}/rollup", s.handleRollupProductKPIs)

	// 后台 RBAC：用户 / 角色 / 菜单（仅 super_admin，见 admin_rbac.go）
	mux.HandleFunc("GET /v1/admin/users", s.handleListUsers)
	mux.HandleFunc("POST /v1/admin/users", s.handleCreateUser)
	mux.HandleFunc("PATCH /v1/admin/users/{id}", s.handleUpdateUser)
	mux.HandleFunc("DELETE /v1/admin/users/{id}", s.handleDeleteUser)
	mux.HandleFunc("GET /v1/admin/roles", s.handleListRoles)
	mux.HandleFunc("POST /v1/admin/roles", s.handleCreateRole)
	mux.HandleFunc("PATCH /v1/admin/roles/{code}", s.handleUpdateRole)
	mux.HandleFunc("DELETE /v1/admin/roles/{code}", s.handleDeleteRole)
	mux.HandleFunc("GET /v1/admin/roles/{code}/menus", s.handleGetRoleMenus)
	mux.HandleFunc("PUT /v1/admin/roles/{code}/menus", s.handleSetRoleMenus)
	mux.HandleFunc("GET /v1/admin/menus", s.handleListMenus)
	mux.HandleFunc("POST /v1/admin/menus", s.handleCreateMenu)
	mux.HandleFunc("PATCH /v1/admin/menus/{code}", s.handleUpdateMenu)
	mux.HandleFunc("DELETE /v1/admin/menus/{code}", s.handleDeleteMenu)

	// 运行时设置（决策缓存等后台可配置项）
	mux.HandleFunc("GET /v1/admin/settings/{key}", s.handleGetSetting)
	mux.HandleFunc("PATCH /v1/admin/settings/{key}", s.handleUpdateSetting)

	// 看板实时流（阶段 3.1）：SSE 推送 KPI 卡 / 广告主监控 / 广告位状态
	mux.HandleFunc("GET /v1/admin/metrics/stream", s.handleMetricsStream)

	// 健康检查（Fly health check）
	mux.HandleFunc("GET /healthz", s.handleHealthz)

	// Swagger UI（规范由 swag 生成，见 docs/ 包；文档仅覆盖客户端接口）
	mux.Handle("/swagger/", httpSwagger.WrapHandler)
	return s.loggingMiddleware(mux)
}

// statusRecorder 包装 ResponseWriter，记录响应状态码（默认 200）。
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// loggingMiddleware 对每个请求打印耗时（毫秒）、方法、路径、状态码，
// 便于通过 fly logs 观察哪些接口慢。/healthz 健康检查每 15s 一次，跳过以免刷屏。
func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	skip := map[string]bool{"/healthz": true, "/swagger/": true}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if skip[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.Log.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"dur_ms", time.Since(start).Milliseconds(),
		)
	})
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
	// 队列指标（M2；M3 面板数据源：Pending 持续增长 = 消费卡住）
	QueueBuffered int64 `json:"queue_buffered"`
	QueuePending  int64 `json:"queue_pending"`
	QueueDropped  int64 `json:"queue_dropped"`
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
	if s.Queue != nil {
		qs := s.Queue.Stats()
		resp.QueueBuffered, resp.QueuePending, resp.QueueDropped = qs.Buffered, qs.Pending, qs.Dropped
	}
	writeJSON(w, http.StatusOK, resp)
}
