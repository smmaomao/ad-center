// Package cache 决策结果缓存。
//
// 后端为 Redis（多实例共享、未来水平扩展免改架构）；未配置 REDIS_URL 或连接
// 失败时降级为 Noop（实时计算，仅丢失缓存收益，不影响正确性）。
//
// 值以 JSON 字节存储（engine.Response 序列化），由调用方负责序列化/反序列化，
// 保持本包与 engine 解耦。
package cache

import (
	"context"
	"time"
)

// DecisionCache 决策结果缓存抽象。
type DecisionCache interface {
	// Get 命中返回 (value, true)；未命中或出错返回 (nil, false)。
	Get(ctx context.Context, key string) ([]byte, bool, error)
	// Set 写入并设置过期（TTL）。
	Set(ctx context.Context, key string, val []byte, ttl time.Duration) error
}

// Noop 不缓存（降级模式）。Get 永远未命中，Set 为空操作。
type Noop struct{}

func (Noop) Get(context.Context, string) ([]byte, bool, error) { return nil, false, nil }
func (Noop) Set(context.Context, string, []byte, time.Duration) error {
	return nil
}
