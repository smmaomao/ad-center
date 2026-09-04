// adcenter 服务入口：装配 DB / 配置缓存 / 决策引擎 / 频控预算 / HTTP。
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"adcenter/internal/api"
	"adcenter/internal/budget"
	"adcenter/internal/config"
	"adcenter/internal/engine"
	"adcenter/internal/frequency"
	"adcenter/internal/metrics"
	"adcenter/internal/store"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(log)

	// .env（本地开发便利；生产用真实环境变量）
	loadDotEnv(".env")

	dsn := getenv("DATABASE_URL", "postgres://postgres:postgres@127.0.0.1:54322/postgres?sslmode=disable")
	port := getenv("PORT", "8080")
	internalKey := os.Getenv("INTERNAL_API_KEY")
	if internalKey == "" {
		log.Warn("INTERNAL_API_KEY not set, admin API disabled")
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

	// ② 配置缓存：全量加载 + LISTEN/NOTIFY + 60s 对账
	cache, err := config.NewCache(ctx, st, log)
	if err != nil {
		log.Error("config cache init failed", "err", err)
		os.Exit(1)
	}
	go cache.RunListener(ctx, dsn, log)
	go cache.RunReconcile(ctx, 60*time.Second)

	// ③ 预算：DB 为每日起点，进程内维护，ledger 异步落库
	balances, err := st.LoadBudgetBalances(ctx)
	if err != nil {
		log.Error("budget load failed", "err", err)
		os.Exit(1)
	}
	bm := map[string][2]float64{}
	for _, b := range balances {
		bm[b.AdvertiserID] = [2]float64{b.DailyBudget, b.SpentToday}
	}
	budgetCtrl := budget.NewMemory(bm, func(advertiserID, op string, amount float64) {
		// 异步落流水：短超时防阻塞预算路径
		c, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = st.WriteLedger(c, advertiserID, "", op, amount)
	})

	// ④ 频控 + 引擎 + 指标
	freqStore := frequency.NewMemory(24 * time.Hour)
	eng := &engine.Engine{Freq: freqStore, Budget: budgetCtrl}
	agg := metrics.New()

	// ⑤ HTTP 装配
	srv := &api.Server{
		Cache: cache, Engine: eng, Store: st, Metrics: agg,
		Budget: budgetCtrl, InternalKey: internalKey, Log: log,
	}
	srv.SetEventWriter(api.NewEventWriter(st, log))
	srv.RunEventWriter(ctx)
	go agg.RunFlushLoop(ctx, st, log)

	// ⑥ 频控过期清理（每 10 分钟）
	go func() {
		t := time.NewTicker(10 * time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				freqStore.Prune(time.Now())
			}
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
