package budget

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestRedisCtrlContract Redis 实现跑与内存版**同一套**契约用例
// （ARCHITECTURE.md §5.3.1 的迁移前置保险）。
//
// 需要 Redis：TEST_REDIS_URL 未设置则跳过（CI 无 Redis 也能通过）。
// 本地/联调执行：
//
//	TEST_REDIS_URL=redis://localhost:6379/3 go test ./internal/budget/... -run Contract
func TestRedisCtrlContract(t *testing.T) {
	url := os.Getenv("TEST_REDIS_URL")
	if url == "" {
		t.Skip("TEST_REDIS_URL not set, skip Redis contract test")
	}

	ctrl, err := NewRedis(url, "adcenter:test:budget:", nil)
	if err != nil {
		t.Fatalf("NewRedis: %v", err)
	}
	if err := ctrl.Ping(context.Background()); err != nil {
		t.Fatalf("redis ping: %v", err)
	}
	flushBudgetKeys(t, ctrl)
	t.Cleanup(func() { flushBudgetKeys(t, ctrl); _ = ctrl.Close() })

	now := time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC) // Jakarta 17:00
	ctrl.SetNow(func() time.Time { return now })

	// 初始状态等价于内存版的构造入参（DB 全量快照灌入）
	ctrl.SyncBalances(map[string][2]float64{
		"adv1": {100, 0},
		"adv2": {100, 95},
		"adv3": {50, 0},
		"adv4": {30, 0},
		"adv5": {20, 0},
		"adv6": {10, 10},
	})

	runCtrlContract(t, ctrl, func(t time.Time) { now = t }, func(all map[string][2]float64) {
		ctrl.SyncBalances(all)
	})
}

// flushBudgetKeys 清空本次测试使用的键，保证用例可重复执行。
func flushBudgetKeys(t *testing.T, c *Redis) {
	t.Helper()
	ctx := context.Background()
	var cursor uint64
	for {
		keys, next, err := c.client.Scan(ctx, cursor, c.prefix+"{bud}:*", 500).Result()
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		if len(keys) > 0 {
			if err := c.client.Del(ctx, keys...).Err(); err != nil {
				t.Fatalf("del: %v", err)
			}
		}
		cursor = next
		if cursor == 0 {
			return
		}
	}
}
