package config

import (
	"context"
	"log/slog"
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
type Cache struct {
	loader Loader
	snap   atomic.Pointer[Snapshot]
	log    *slog.Logger
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
func (c *Cache) Reload(ctx context.Context) error {
	s, err := c.loader.LoadSnapshot(ctx)
	if err != nil {
		c.log.Error("config reload failed, keeping old snapshot", "err", err)
		return err
	}
	c.snap.Store(s)
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
			if err := c.Reload(ctx); err == nil {
				c.log.Debug("config reconcile ok")
			}
		}
	}
}
