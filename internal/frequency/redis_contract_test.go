package frequency

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestRedisStoreContract Redis 实现跑与内存版**同一套**契约用例
// （ARCHITECTURE.md §5.3.1 的迁移前置保险）。
//
// 需要 Redis：TEST_REDIS_URL 未设置则跳过（CI 无 Redis 也能通过）。
// 本地/联调执行：
//
//	TEST_REDIS_URL=redis://localhost:6379/3 go test ./internal/frequency/... -run Contract
func TestRedisStoreContract(t *testing.T) {
	url := os.Getenv("TEST_REDIS_URL")
	if url == "" {
		t.Skip("TEST_REDIS_URL not set, skip Redis contract test")
	}

	store, err := NewRedis(url, "adcenter:test:fr:", 24*time.Hour)
	if err != nil {
		t.Fatalf("NewRedis: %v", err)
	}
	if err := store.Ping(context.Background()); err != nil {
		t.Fatalf("redis ping: %v", err)
	}
	flushFreqKeys(t, store)
	t.Cleanup(func() { flushFreqKeys(t, store); _ = store.Close() })

	runStoreContract(t, store)
}

// flushFreqKeys 清空本次测试使用的键（含 INCR 序列键），保证用例可重复执行。
func flushFreqKeys(t *testing.T, s *RedisStore) {
	t.Helper()
	ctx := context.Background()
	var cursor uint64
	for {
		keys, next, err := s.client.Scan(ctx, cursor, s.prefix+"{fr}:*", 500).Result()
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		if len(keys) > 0 {
			if err := s.client.Del(ctx, keys...).Err(); err != nil {
				t.Fatalf("del: %v", err)
			}
		}
		cursor = next
		if cursor == 0 {
			return
		}
	}
}
