// adcenter 服务入口：装配 DB / 配置缓存 / 决策引擎 / 频控预算 / HTTP。
//
//	@title           广告投放中心 客户端接口
//	@version         1.0
//	@description     客户端 SDK 对接用接口：批量获取广告、曝光/点击/完播埋点。所有接口通过 HTTP Header `X-Api-Key` 鉴权（值为后台分配的 App API Key）。
//	@schemes         https http
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"adcenter"
	"adcenter/internal/api"
	"adcenter/internal/budget"
	cachestore "adcenter/internal/cache"
	"adcenter/internal/config"
	"adcenter/internal/engine"
	"adcenter/internal/frequency"
	"adcenter/internal/metrics"
	"adcenter/internal/migrate"
	"adcenter/internal/queue"
	"adcenter/internal/storage"
	"adcenter/internal/store"

	_ "adcenter/docs" // 注册 swag 生成的 Swagger 规范（/swagger 路由据此提供 UI）
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(log)

	// .env（本地开发便利；生产用真实环境变量）
	loadDotEnv(".env")

	dsn := getenv("DATABASE_URL", "postgres://postgres:postgres@127.0.0.1:54322/postgres?sslmode=disable")
	port := getenv("PORT", "8888")
	internalKey := os.Getenv("INTERNAL_API_KEY")
	if internalKey == "" {
		log.Warn("INTERNAL_API_KEY not set, admin API disabled")
	}
	sessionSecret := os.Getenv("SESSION_SECRET")
	if sessionSecret == "" {
		log.Warn("SESSION_SECRET not set, admin login disabled")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// ① DB
	st, err := store.New(ctx, dsn)
	if err != nil {
		log.Error("store init failed", "err", err)
		os.Exit(1)
	}
	defer st.Close()

	// ①' 数据库迁移：启动即把未执行的迁移应用到当前库（幂等；已在追踪表的跳过）。
	// 见 internal/migrate。可用 RUN_MIGRATIONS=false 关闭（如只想手动跑 cmd/migrate）。
	if os.Getenv("RUN_MIGRATIONS") != "false" {
		if err := migrate.Up(ctx, st.Pool(), adcenter.MigrationFS, log); err != nil {
			log.Error("db migration failed", "err", err)
			os.Exit(1)
		}
	}

	// ② 配置缓存：全量加载（监听/对账协程在预算就绪后再启动，见 ③'）
	cache, err := config.NewCache(ctx, st, log)
	if err != nil {
		log.Error("config cache init failed", "err", err)
		os.Exit(1)
	}

	redisURL := os.Getenv("REDIS_URL")
	// 频控 / 预算 / 队列的默认后端：只要配了 REDIS_URL 就默认走 Redis
	// （多实例共享、重启不丢），与决策缓存、bid 登记表共用同一开关；本地开发
	// 不配 REDIS_URL 时退回进程内实现，或显式 STATE_STORE=memory /
	// QUEUE_DRIVER=memory 强制内存。
	// REDIS_URL 支持 /<db> 指定库号（go-redis ParseURL 解析）。库号约定：本地多
	// 项目共用一个 Redis，按 DB index 隔离，广告中心本地用 /10；线上各项目有
	// 独立 Redis 实例，REDIS_URL 直连即可，测试/正式如需共用实例再用 index 区分。预算/频控走 Redis 时若连接失败会**启动即退出**
	// （fail-loud），避免"以为分布式、其实各实例各管各的"超发资损（见 SCALING.md §6）。
	defState := "memory"
	defQueue := "memory"
	if redisURL != "" {
		defState = "redis"
		defQueue = "redis"
	}
	stateStore := getenv("STATE_STORE", defState)
	// 事件队列驱动（M2，SCALING.md §2）：redis = Streams + 消费端批量落库 + 流水聚合
	queueDriver := getenv("QUEUE_DRIVER", defQueue)

	balances, err := st.LoadBudgetBalances(ctx)
	if err != nil {
		log.Error("budget load failed", "err", err)
		os.Exit(1)
	}
	bm := map[string][2]float64{}
	for _, b := range balances {
		bm[b.CampaignID] = [2]float64{b.DailyBudget, b.SpentToday}
	}

	ledgerFn := func(id, op string, amount float64) {
		// M2：队列走 Redis 时，逐笔 deduct 不再在请求路径写 DB——金额随事件
		// 进入队列，由消费端按 (广告主, 小时) 聚合成一行（见 SCALING.md §2）。
		// daily_reset / calibrate 不经过事件流，仍在此直写。
		if queueDriver == "redis" && op == "deduct" {
			return
		}
		// 落流水：短超时防阻塞预算路径
		c, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = st.WriteLedger(c, id, "", op, amount)
	}

	// 预算控制器同时承担 Ctrl（扣费/查询）与 Syncer（配置热更新同步）两个契约。
	type budgetStore interface {
		budget.Ctrl
		budget.Syncer
	}
	var budgetCtrl budgetStore = budget.NewMemory(bm, ledgerFn)
	if stateStore == "redis" {
		if redisURL == "" {
			log.Error("STATE_STORE=redis but REDIS_URL not set")
			os.Exit(1)
		}
		bc, err := budget.NewRedis(redisURL, "", ledgerFn)
		if err != nil {
			log.Error("budget redis init failed", "err", err)
			os.Exit(1)
		}
		if err := bc.Ping(ctx); err != nil {
			log.Error("budget redis ping failed", "err", err)
			_ = bc.Close()
			os.Exit(1)
		}
		// 用启动快照初始化：已存在的广告主只刷新日预算，保留 Redis 当日已耗
		// （进程重启不丢消耗，与内存版"启动时灌入"语义对齐）。
		bc.SyncBalances(bm)
		budgetCtrl = bc
		log.Info("budget store enabled (redis)")
	}

	// 广告主总钱包：启动灌入**已启用钱包**的余额（未启用的存量广告主不在内 →
	// 不受总余额限制，行为与上线前一致）。余额真相在 DB，消费端扣费 / 后台充值
	// 都会落库，此处只是进程内实时只读闸的初始副本。
	if wallets, werr := st.LoadWalletBalances(ctx); werr != nil {
		log.Error("wallet load failed", "err", werr)
		os.Exit(1)
	} else {
		budgetCtrl.SyncWallets(wallets)
		log.Info("wallets loaded", "count", len(wallets))
	}

	// ③' 配置刷新 → 预算同步：后台新建广告主 / 调整日预算无需重启即生效。
	// 必须在启动 listener 与 reconcile 之前注册，否则首轮刷新时预算表还没就绪。
	// 预算只绑 advertisers 表：仅广告主相关变更、定时对账、断线重连才需重查
	// 预算；apps/slots/creatives 的变更不触碰日预算，跳过整表查询（每次 NOTIFY
	// 都全量 LoadBudgetBalances 是浪费且会与扣费路径抢同一把预算锁）。
	cache.OnChange(func(reason string) {
		if reason == "reconcile" || reason == "reconnect" || reason == "initial" ||
			strings.HasPrefix(reason, "notify:advertisers") {
			syncBudgets(ctx, st, budgetCtrl, log, reason)
		}
	})
	go cache.RunListener(ctx, dsn, log)
	go cache.RunReconcile(ctx, 60*time.Second)

	// ④ 频控 + 引擎 + 指标
	var freqStore frequency.Store = frequency.NewMemory(24 * time.Hour)
	if stateStore == "redis" {
		fs, err := frequency.NewRedis(redisURL, "", 24*time.Hour)
		if err != nil {
			log.Error("frequency redis init failed", "err", err)
			os.Exit(1)
		}
		if err := fs.Ping(ctx); err != nil {
			log.Error("frequency redis ping failed", "err", err)
			_ = fs.Close()
			os.Exit(1)
		}
		freqStore = fs
		log.Info("frequency store enabled (redis)")
	}
	// ④' 全局疲劳度已移除：任务级频控（engine + frequency）统一在 impression 时计数。
	eng := &engine.Engine{Freq: freqStore, Budget: budgetCtrl}
	agg := metrics.New()

	// ④' R2 只读签名器（决策下发 media_url 用；未配置则响应不含签名地址）
	var r2Signer *storage.Signer
	if v := os.Getenv("R2_ACCOUNT_ID"); v != "" {
		r2Signer = storage.New(v,
			os.Getenv("R2_BUCKET"),
			os.Getenv("R2_ACCESS_KEY_ID"),
			os.Getenv("R2_SECRET_ACCESS_KEY"))
		log.Info("r2 signer enabled", "bucket", os.Getenv("R2_BUCKET"))
		if base := os.Getenv("R2_CDN_BASE"); base != "" {
			// R2 自定义域（桶绑域名，路径不含桶）设 R2_CDN_BUCKET_IN_PATH=false；
			// Worker 代理若仍按 /bucket/key 转发则保持默认 true。
			bucketInPath := os.Getenv("R2_CDN_BUCKET_IN_PATH") != "false"
			r2Signer.SetCDN(base, bucketInPath)
			log.Info("r2 cdn base enabled", "base", base, "bucketInPath", bucketInPath)
		}
		// 预签名有效期（默认 24h）。CDN 边缘长缓存下，签名仅在首次回源/直连 R2 时真正验。
		if ttl := os.Getenv("R2_SIGN_TTL"); ttl != "" {
			if d, err := time.ParseDuration(ttl); err == nil {
				r2Signer.SetSignTTL(d)
				log.Info("r2 sign ttl configured", "ttl", ttl)
			} else {
				log.Warn("invalid R2_SIGN_TTL, using default 24h", "err", err)
			}
		}
	} else {
		log.Warn("R2_* not set, decision response will not include media_url")
	}

	// ④'' 决策结果缓存（Redis，多实例共享；未配置 REDIS_URL 或连接失败则
	// 降级为实时计算，仅丢失缓存收益，不影响正确性）。
	var decisionCache cachestore.DecisionCache = cachestore.Noop{}
	if redisURL != "" {
		rc, err := cachestore.NewRedis(redisURL, "")
		if err != nil {
			log.Warn("redis init failed, decision cache disabled", "err", err)
		} else if err := rc.Ping(ctx); err != nil {
			log.Warn("redis ping failed, decision cache disabled", "err", err)
			_ = rc.Close()
		} else {
			decisionCache = rc
			log.Info("decision cache enabled (redis)")
		}
	} else {
		log.Warn("REDIS_URL not set, decision cache disabled (live compute)")
	}

	// ⑤ HTTP 装配
	srv := &api.Server{
		Cache: cache, Engine: eng, Store: st, Metrics: agg,
		Budget: budgetCtrl, InternalKey: internalKey, SessionSecret: sessionSecret, Log: log,
		Storage: r2Signer, DecisionCache: decisionCache,
		Clicks: api.NewClickResolver(st), // clickid 归因反查（落库实现）
		Freq:   freqStore,                // 任务级频控：与引擎共用同一实例
	}
	// ⑤' 事件队列：memory = 进程内 channel（现状）；redis = Streams + consumer group
	var evtQueue queue.Backend = queue.NewMemory(8192, 500, 2*time.Second)
	if queueDriver == "redis" {
		if redisURL == "" {
			log.Error("QUEUE_DRIVER=redis but REDIS_URL not set")
			os.Exit(1)
		}
		q, err := queue.NewRedis(redisURL, "", "adcenter", 500)
		if err != nil {
			log.Error("event queue redis init failed", "err", err)
			os.Exit(1)
		}
		evtQueue = q
		log.Info("event queue enabled (redis streams)")
	}
	srv.Queue = evtQueue
	// 下发交易登记表（客户端接口 bid_id → 上下文，TTL 30min）：
	// 优先 Redis（跨实例共享、重启不丢），未配置 REDIS_URL 或连接失败则降级内存。
	srv.Bids = api.NewMemBidStore()
	if redisURL != "" {
		if rb, err := api.NewRedisBidStore(redisURL, 30*time.Minute); err != nil {
			log.Warn("redis init failed, bid registry falls back to in-memory", "err", err)
		} else if err := rb.Ping(ctx); err != nil {
			log.Warn("redis ping failed, bid registry falls back to in-memory", "err", err)
			_ = rb.Close()
		} else {
			srv.Bids = rb
			log.Info("bid registry enabled (redis)")
		}
	}
	// 消费失败必须可见：Redis 后端不 ACK（留在 PEL 重试），此处负责打日志
	go evtQueue.Run(ctx, func(ctx context.Context, batch []store.AdEvent) error {
		if err := srv.ConsumeEvents(ctx, batch); err != nil {
			log.Error("event consume failed (will retry)", "err", err, "n", len(batch))
			return err
		}
		return nil
	})

	go agg.RunFlushLoop(ctx, st, log)

	// ⑥ 频控过期清理（每 10 分钟）
	//
	// 仅内存版需要：Redis 版状态带 TTL（maxWindow），过期由 Redis 承担。
	go func() {
		pruner, ok := freqStore.(interface{ Prune(time.Time) })
		if !ok {
			return
		}
		t := time.NewTicker(10 * time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				pruner.Prune(time.Now())
			}
		}
	}()

	// ⑥' 分区守护：每天 03:00（Jakarta）预建后续月份分区。
	//
	// 启动时不跑——分区已由 migration 000014 一次性补建 12 个月，定时任务只负责
	// 长期续期。多实例同时跑是安全的：advisory lock + IF NOT EXISTS（见
	// store.EnsurePartitions），不会出现"两台一起建、互相报错"。
	go func() {
		loc := time.FixedZone("WIB", 7*60*60)
		for {
			now := time.Now().In(loc)
			next := time.Date(now.Year(), now.Month(), now.Day(), 3, 0, 0, 0, loc)
			if !next.After(now) {
				next = next.AddDate(0, 0, 1)
			}
			select {
			case <-time.After(next.Sub(now)):
			case <-ctx.Done():
				return
			}
			ensurePartitions(ctx, st, log)
		}
	}()

	// ⑥'' 分区 TTL 清理：每天 04:00（Jakarta）删除保留窗口之外的旧子分区。
	//
	// 与 ⑥' 配对——⑥' 负责"往前建"，本任务负责"往后清"，避免分区无限堆积。
	// 保留月数由 PARTITION_RETENTION_MONTHS 控制（默认 13，覆盖一个完整年度 +
	// 跨年同比）；当前 dev 库最旧分区即当前月，默认窗口下不会误删任何数据。
	// 多实例并发安全：advisory lock + DROP TABLE IF EXISTS（见 store.DropOldPartitions）。
	go func() {
		retain := 13
		if v := os.Getenv("PARTITION_RETENTION_MONTHS"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				retain = n
			}
		}
		loc := time.FixedZone("WIB", 7*60*60)
		for {
			now := time.Now().In(loc)
			next := time.Date(now.Year(), now.Month(), now.Day(), 4, 0, 0, 0, loc)
			if !next.After(now) {
				next = next.AddDate(0, 0, 1)
			}
			select {
			case <-time.After(next.Sub(now)):
			case <-ctx.Done():
				return
			}
			dropOldPartitions(ctx, st, log, retain)
		}
	}()

	httpSrv := &http.Server{
		Addr:              ":" + port,
		Handler:           srv.NewRouter(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		log.Info("adcenter listening", "port", port)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("http server failed", "err", err)
			stop()
		}
	}()

	// ⑦ 优雅关停：SIGTERM 后 drain 在途请求（Fly 发版用）
	<-ctx.Done()
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Error("graceful shutdown failed", "err", err)
	}
	log.Info("bye")
}

// ensurePartitions 预建后续月份分区（幂等；失败只告警，下一天自动重试）。
func ensurePartitions(ctx context.Context, st *store.Store, log *slog.Logger) {
	c, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	// 2 = 保证"下月 + 下下月"就绪，即使某天任务失败也有一天缓冲
	if err := st.EnsurePartitions(c, 2); err != nil {
		log.Error("ensure partitions failed", "err", err)
		return
	}
	log.Info("partitions ensured", "months_ahead", 2)
}

// dropOldPartitions 删除保留窗口之外的旧子分区（幂等；失败只告警，下一天重试）。
func dropOldPartitions(ctx context.Context, st *store.Store, log *slog.Logger, retain int) {
	c, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := st.DropOldPartitions(c, retain); err != nil {
		log.Error("drop old partitions failed", "err", err)
		return
	}
	log.Info("old partitions dropped", "retain_months", retain)
}

// syncBudgets 把 DB 的最新日预算对齐到进程内预算表（B2/B3）。
//
// 触发时机：任何一次配置快照刷新成功（NOTIFY 秒级 / 60s 对账兜底）。
// 失败只告警不中断——配置刷新本身已成功，预算晚一拍对齐远好过让回调
// panic 掉 listener 协程。
func syncBudgets(ctx context.Context, st *store.Store, ctrl budget.Syncer, log *slog.Logger, reason string) {
	c, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	balances, err := st.LoadBudgetBalances(c)
	if err != nil {
		log.Error("budget sync failed", "reason", reason, "err", err)
		return
	}
	bm := make(map[string][2]float64, len(balances))
	for _, b := range balances {
		bm[b.CampaignID] = [2]float64{b.DailyBudget, b.SpentToday}
	}
	ctrl.SyncBalances(bm)

	// 广告主总钱包同步（充值 / 扣费后对账）。失败不回退日预算——两者独立，
	// 钱包晚一拍对齐即可。
	if wallets, werr := st.LoadWalletBalances(c); werr != nil {
		log.Error("wallet sync failed", "reason", reason, "err", werr)
	} else {
		ctrl.SyncWallets(wallets)
	}
	log.Debug("budget synced", "reason", reason, "advertisers", len(bm))
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// loadDotEnv 极简 .env 加载（不覆盖已存在的环境变量）。
func loadDotEnv(path string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range splitLines(string(b)) {
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		for i := 0; i < len(line); i++ {
			if line[i] == '=' {
				k, v := line[:i], line[i+1:]
				if os.Getenv(k) == "" {
					_ = os.Setenv(k, trimQuotes(v))
				}
				break
			}
		}
	}
}

func trimQuotes(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			line := s[start:i]
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			out = append(out, line)
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}
