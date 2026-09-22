// Package api 承载 HTTP 层：客户端决策 API、事件上报、管理 API。
// 路由统一在此注册，handler 按域拆分文件。
package api

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"runtime"
	"strings"
	"time"

	"adcenter/internal/budget"
	"adcenter/internal/cache"
	"adcenter/internal/config"
	"adcenter/internal/engine"
	"adcenter/internal/frequency"
	"adcenter/internal/metrics"
	"adcenter/internal/queue"
	"adcenter/internal/storage"
	"adcenter/internal/store"
	httpSwagger "github.com/swaggo/http-swagger"
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
	// 跨样式批量决策时由 API 层预取一次、复用，避免热路径重复读 Redis。
	BudgetSnapshot(snap *config.Snapshot) map[string][2]float64
	WalletBalances(snap *config.Snapshot) map[string]float64
	DecideWith(snap *config.Snapshot, req engine.Request, campBudgets map[string][2]float64, wallets map[string]float64) engine.Response
}

// Server 聚合全部运行依赖（main 装配后注入）。
type Server struct {
	Cache         *config.Cache
	Engine        Decider
	Store         *store.Store
	Metrics       *metrics.Agg
	Budget        budget.Ctrl
	InternalKey   string // 管理 API 内部密钥（BFF 共享）
	SessionSecret string // 后台登录会话令牌签名密钥（HMAC，migration 000044）
	Log           *slog.Logger
	Storage       *storage.Signer     // R2 预签名（nil = 未配置，下发不含 media_url）
	Clicks        ClickResolver       // clickid 归因反查（S2S 转化用）；nil = 未接入，转化只确认不扣费
	DecisionCache cache.DecisionCache // 决策结果缓存（Redis；nil/Noop = 实时计算）
	Queue         queue.Backend       // 事件队列（M2：memory / redis streams）；nil = 丢弃事件
	Freq          frequency.Store     // 任务级频控（与引擎共用同一实例）
	Bids          BidStore            // 下发交易上下文登记表（客户端接口 bid_id → 上下文，Redis/内存）
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

	// 归因方 S2S 转化回调（无 App Key，暂时无独立密钥）。
	// 同时支持 GET 与 POST：参数既可走 query，也可走 JSON / 表单 body —— 适配
	// 不同归因平台与中介（berealads 等）各自的回传方式。
	mux.HandleFunc("GET /v1/s2s/event", s.handleS2SEvent)
	mux.HandleFunc("POST /v1/s2s/event", s.handleS2SEvent)

	// 管理 API（内部密钥 + RBAC）
	mux.HandleFunc("GET /v1/admin/advertisers", s.handleListAdvertisers)
	mux.HandleFunc("POST /v1/admin/advertisers", s.handleCreateAdvertiser)
	mux.HandleFunc("GET /v1/admin/advertisers/{id}", s.handleGetAdvertiser)
	mux.HandleFunc("PATCH /v1/admin/advertisers/{id}", s.handleUpdateAdvertiser)
	mux.HandleFunc("DELETE /v1/admin/advertisers/{id}", s.handleDeleteAdvertiser)
	mux.HandleFunc("GET /v1/admin/advertisers/{id}/rollup", s.handleRollupAdvertiserKPIs)
	// 广告主总钱包（充值 - 扣费；余额为投放硬顶）
	mux.HandleFunc("GET /v1/admin/advertisers/{id}/wallet", s.handleGetWallet)
	mux.HandleFunc("GET /v1/admin/advertisers/{id}/wallet/flow", s.handleListWalletFlow)
	mux.HandleFunc("POST /v1/admin/advertisers/{id}/wallet/recharge", s.handleRecharge)
	mux.HandleFunc("POST /v1/admin/advertisers/{id}/wallet/adjust", s.handleAdjustWallet)
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

	// 后台自管登录（migration 000044）：开放端点，签发 HMAC 会话令牌
	mux.HandleFunc("POST /v1/admin/login", s.handleLogin)
	mux.HandleFunc("GET /v1/admin/me", s.handleAdminMe)

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

	// 看板实时流（已暂停使用，暂不推送 SSE）
	mux.HandleFunc("GET /v1/admin/metrics/stream", s.handleMetricsStream)

	// 健康检查（Fly health check）
	mux.HandleFunc("GET /healthz", s.handleHealthz)

	// Swagger UI（规范由 swag 生成，见 docs/ 包；文档仅覆盖客户端接口）
	mux.Handle("/swagger/", httpSwagger.WrapHandler)
	return s.loggingMiddleware(mux)
}

// statusRecorder 包装 ResponseWriter，记录响应状态码（默认 200）。
// 对 4xx/5xx 额外截留一小段响应体（错误原因），供日志打印——正常响应不缓冲。
type statusRecorder struct {
	http.ResponseWriter
	status  int
	capture bool
	body    bytes.Buffer
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.capture = code >= 400
	r.ResponseWriter.WriteHeader(code)
}

// Write 透传响应体；仅当本次为 4xx/5xx 时截留前 1KB（错误信息本身很小），
// 避免对大响应（如列表、流式）做无谓缓冲。
func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.capture && r.body.Len() < 1024 {
		r.body.Write(b)
	}
	return r.ResponseWriter.Write(b)
}

// Flush 透传给底层 ResponseWriter（若支持），否则静默忽略。
// statusRecorder 内嵌的是 http.ResponseWriter 接口（不含 Flush），不显式转发会导致
// SSE 等流式端点里 w.(http.Flusher) 断言失败、返回 "streaming unsupported"（见 metrics_stream.go）。
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// loggingMiddleware 对每个请求打印耗时（毫秒）、方法、路径、状态码，
// 便于通过 fly logs 观察哪些接口慢。/healthz 健康检查每 60s 一次，跳过以免刷屏。
// 4xx/5xx 时附带应用返回的错误原因（resp），便于直接定位失败原因。
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
		attrs := []any{
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"dur_ms", time.Since(start).Milliseconds(),
		}
		if rec.status >= 400 {
			attrs = append(attrs, "resp", strings.TrimSpace(rec.body.String()))
		}
		s.Log.Info("request", attrs...)
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
	MetricsBuffer int `json:"metrics_buffer"`
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
