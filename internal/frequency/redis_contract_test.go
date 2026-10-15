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

// TestRedisCheckBatchMatchesCheck 批量只读检查必须与逐条 Check 结果一致。
//
// Lua 是复制 checkOnlyScript 的逻辑重写的，最容易出的是参数游标错位（窗口数可变），
// 故用「造状态 → 批量 vs 逐条对拍」来锁住语义，而不是断言某个具体期望值。
func TestRedisCheckBatchMatchesCheck(t *testing.T) {
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

	now := time.Now()
	items := []CheckItem{
		// 已记 2 次、上限 2 → 应被挡
		{SlotID: "cr1", AdvertiserID: "camp1", Policy: AdvPolicy{Windows: []Window{{WindowMinutes: 20, MaxCount: 2}}}},
		// 上限 1 但只记 0 次 → 放行；带疲劳窗口（覆盖 fatigueN 分支与参数游标）
		{SlotID: "cr2", AdvertiserID: "camp2", Policy: AdvPolicy{Windows: []Window{{WindowMinutes: 20, MaxCount: 1}}, FatigueN: 2}},
		// 无窗口（不限频）→ 放行，且验证 wcount=0 时游标不越界
		{SlotID: "cr3", AdvertiserID: "camp3", Policy: AdvPolicy{}},
		// 多窗口 + 日频控
		{SlotID: "cr4", AdvertiserID: "camp4", Policy: AdvPolicy{Windows: []Window{
			{WindowMinutes: 20, MaxCount: 3}, {WindowMinutes: 1440, MaxCount: 1},
		}}},
	}
	for i := 0; i < 2; i++ {
		store.Record("app", "dev", "cr1", "camp1", items[0].Policy, now)
	}
	store.Record("app", "dev", "cr4", "camp4", items[3].Policy, now) // 日频控上限 1 → 已达标

	got := store.CheckBatch("app", "dev", items, now)
	if len(got) != len(items) {
		t.Fatalf("CheckBatch 返回长度 = %d，期望 %d", len(got), len(items))
	}
	blocked := false
	for i, it := range items {
		want := store.Check("app", "dev", it.SlotID, it.AdvertiserID, it.Policy, now)
		if got[i] != want {
			t.Errorf("候选 %d(%s): 批量=%v 逐条=%v", i, it.SlotID, got[i], want)
		}
		if !got[i] {
			blocked = true
		}
	}
	if !blocked {
		t.Fatal("用例设计失效：期望至少一个候选被频控挡住")
	}
	// 空入参：返回 nil（调用方据此回退逐条 Check）
	if r := store.CheckBatch("app", "dev", nil, now); r != nil {
		t.Fatalf("空入参应返回 nil，实际 %v", r)
	}
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
