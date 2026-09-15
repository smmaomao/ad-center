package config

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// Loader 配置加载方（store 实现）：按表粒度全量加载。
// 重新加载以表为单位——NOTIFY payload 携带表名，只重载受影响的表。
type Loader interface {
	LoadSnapshot(ctx context.Context) (*Snapshot, error)
}

// Cache 配置缓存：atomic 指针持有不可变快照，读路径无锁。
//
// 刷新失败时保留旧快照继续服务（Prebid RateConverter 的 staleness 策略），
// 仅日志告警——配置旧一点好过没有。
// OnReloadFunc 快照刷新成功后的回调。reason 为触发原因，便于日志与排障
// （"notify:<表名>" 来自 LISTEN/NOTIFY，"reconcile" 来自 60s 对账，"initial" 为首次加载）。
//
// 用途：配置快照之外的进程内状态（如 budget.Memory 的日预算表）需要跟随
// 配置变更一起刷新，否则会出现"配置热更新了、依赖它的状态没有"的不一致
// （详见 budget.Syncer 注释）。回调在刷新快照之后触发，实现必须非阻塞或
// 自带超时——它运行在 listener / reconcile 协程上。
type OnReloadFunc func(reason string)

type Cache struct {
	loader Loader
	snap   atomic.Pointer[Snapshot]
	log    *slog.Logger

	mu       sync.RWMutex
	onReload []OnReloadFunc
}

// OnChange 注册刷新回调（应在服务启动阶段、RunListener/RunReconcile 之前调用）。
func (c *Cache) OnChange(f OnReloadFunc) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onReload = append(c.onReload, f)
}

// fireReload 通知所有回调（快照已替换完成）。
func (c *Cache) fireReload(reason string) {
	c.mu.RLock()
	fns := c.onReload
	c.mu.RUnlock()
	for _, f := range fns {
		f(reason)
	}
}

// NewCache 创建缓存并完成首次全量加载（失败则返回错误，服务不启动）。
func NewCache(ctx context.Context, loader Loader, log *slog.Logger) (*Cache, error) {
	c := &Cache{loader: loader, log: log}
	if err := c.Reload(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

// Snapshot 返回当前配置快照（调用方只读，不得修改内部 map）。
func (c *Cache) Snapshot() *Snapshot { return c.snap.Load() }

// Reload 全量重载（LISTEN 触发 / 60s 对账共用）。
// reason 透传给回调，便于区分是 NOTIFY 还是对账触发的刷新。
func (c *Cache) Reload(ctx context.Context) error {
	return c.reload(ctx, "reload")
}

func (c *Cache) reload(ctx context.Context, reason string) error {
	s, err := c.loader.LoadSnapshot(ctx)
	if err != nil {
		c.log.Error("config reload failed, keeping old snapshot", "err", err)
		return err
	}
	c.snap.Store(s)
	c.fireReload(reason)
	return nil
}

// RunReconcile 定时对账兜底：NOTIFY 丢失（监听连接断线瞬间有变更）时最多 60s 自愈。
func (c *Cache) RunReconcile(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := c.reload(ctx, "reconcile"); err == nil {
				c.log.Debug("config reconcile ok")
			}
		}
	}
}
