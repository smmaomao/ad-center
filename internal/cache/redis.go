package cache

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis 基于 Redis 的决策缓存（多实例共享）。
type Redis struct {
	client *redis.Client
	prefix string
}

// NewRedis 从 REDIS_URL 构造客户端（redis.ParseURL 支持 redis:// 与 rediss://）。
func NewRedis(redisURL, prefix string) (*Redis, error) {
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}
	return &Redis{client: redis.NewClient(opt), prefix: prefix}, nil
}

// Get 读取键；redis.Nil（不存在）视为未命中。
func (r *Redis) Get(ctx context.Context, key string) ([]byte, bool, error) {
	v, err := r.client.Get(ctx, r.prefix+key).Bytes()
	if err == redis.Nil {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return v, true, nil
}

// Set 写入并设过期。
func (r *Redis) Set(ctx context.Context, key string, val []byte, ttl time.Duration) error {
	return r.client.Set(ctx, r.prefix+key, val, ttl).Err()
}

// Ping 校验连通性（main 启动探活用）。
func (r *Redis) Ping(ctx context.Context) error { return r.client.Ping(ctx).Err() }

// Close 关闭连接。
func (r *Redis) Close() error { return r.client.Close() }
