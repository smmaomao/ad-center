package main

import (
	"context"
	"os"
	"testing"
	"time"

	"adcenter/internal/api"
	"adcenter/internal/budget"
	cachestore "adcenter/internal/cache"
	"adcenter/internal/frequency"
	"adcenter/internal/queue"
	"adcenter/internal/store"

	"github.com/redis/go-redis/v9"
)

// 广告中心在共享本地 Redis 上专用 index=10（项目间分库隔离）。
// 线上用 REDIS_URL=redis://host:6379/10 指定；本测试默认也连这个库。
const defaultTestRedisURL = "redis://localhost:6379/10"

func testRedisURL() string {
	if v := os.Getenv("REDIS_URL"); v != "" {
		return v
	}
	return defaultTestRedisURL
}

func mustPing(t *testing.T, url string) *redis.Client {
	t.Helper()
	opt, err := redis.ParseURL(url)
	if err != nil {
		t.Fatalf("parse redis url %q: %v", url, err)
	}
	c := redis.NewClient(opt)
	if err := c.Ping(context.Background()).Err(); err != nil {
		t.Skipf("redis unavailable at %q: %v", url, err)
	}
	return c
}

// db0Client 用同一份连接参数但强制 DB=0，用来验证数据没有泄漏到默认库。
func db0Client(t *testing.T, url string) *redis.Client {
	t.Helper()
	opt, err := redis.ParseURL(url)
	if err != nil {
		t.Fatalf("parse redis url: %v", err)
	}
	opt.DB = 0
	return redis.NewClient(opt)
}

// TestRedisBackedSubsystems 验证从内存切到 Redis 的 5 个功能在 DB10 上真实可用，
// 且 key 隔离在 index=10（不污染默认库）。运行前会清空 DB10，仅用广告中心专用索引。
func TestRedisBackedSubsystems(t *testing.T) {
	url := testRedisURL()
	raw := mustPing(t, url)
	defer raw.Close()
	if err := raw.FlushDB(context.Background()).Err(); err != nil {
		t.Fatalf("flush db: %v", err)
	}
	defer raw.FlushDB(context.Background())

	ctx := context.Background()

	t.Run("Bids", func(t *testing.T) {
		bs, err := api.NewRedisBidStore(url, 30*time.Minute)
		if err != nil {
			t.Fatalf("new bid store: %v", err)
		}
		defer bs.Close()
		if err := bs.Ping(ctx); err != nil {
			t.Fatalf("bid ping: %v", err)
		}
		bc := &api.BidContext{
			AppID: "app1", AdvertiserID: "adv_bid", CreativeID: "cr1",
			CampaignID: "cp1", Style: "rewarded_video", DeviceID: "dev1",
		}
		id := bs.Put(ctx, bc)
		if id == "" {
			t.Fatal("empty bid id")
		}
		got, ok := bs.Get(ctx, id)
		if !ok {
			t.Fatalf("bid %q not found in redis", id)
		}
		if got.CreativeID != "cr1" || got.CampaignID != "cp1" {
			t.Fatalf("bid context mismatch: %+v", got)
		}
		// 验证 key 落在 DB10（前缀 adcenter:bid:）
		n, err := raw.Exists(ctx, "adcenter:bid:"+id).Result()
		if err != nil || n != 1 {
			t.Fatalf("bid key not in DB10: n=%d err=%v", n, err)
		}
		// 验证未泄漏到默认库 DB0
		other := db0Client(t, url)
		defer other.Close()
		if n0, _ := other.Exists(ctx, "adcenter:bid:"+id).Result(); n0 != 0 {
			t.Fatalf("bid key leaked to DB0: n0=%d", n0)
		}
	})

	t.Run("DecisionCache", func(t *testing.T) {
		dc, err := cachestore.NewRedis(url, "")
		if err != nil {
			t.Fatalf("new cache: %v", err)
		}
		defer dc.Close()
		if err := dc.Ping(ctx); err != nil {
			t.Fatalf("cache ping: %v", err)
		}
		if err := dc.Set(ctx, "dec:k1", []byte("v1"), time.Minute); err != nil {
			t.Fatalf("cache set: %v", err)
		}
		v, ok, err := dc.Get(ctx, "dec:k1")
		if err != nil || !ok || string(v) != "v1" {
			t.Fatalf("cache get mismatch: v=%q ok=%v err=%v", v, ok, err)
		}
	})

	t.Run("Frequency", func(t *testing.T) {
		fs, err := frequency.NewRedis(url, "", 24*time.Hour)
		if err != nil {
			t.Fatalf("new freq: %v", err)
		}
		defer fs.Close()
		if err := fs.Ping(ctx); err != nil {
			t.Fatalf("freq ping: %v", err)
		}
		now := time.Now()
		pol := frequency.AdvPolicy{Windows: []frequency.Window{{WindowMinutes: 1440, MaxCount: 1}}}
		if !fs.CheckAndIncr("app", "dev", "slot", "advA", pol, now) {
			t.Fatal("first adv freq should allow")
		}
		if fs.CheckAndIncr("app", "dev", "slot", "advA", pol, now) {
			t.Fatal("second adv freq should block")
		}
		if !fs.CheckAndIncr("app", "dev", "slot", "advB", pol, now) {
			t.Fatal("different advertiser should allow")
		}
		slotPol := frequency.SlotPolicy{Interval: 0, DailyLimit: 1}
		if !fs.CheckSlot("app", "dev", "slot2", slotPol, 1, now) {
			t.Fatal("first slot daily check should allow")
		}
		fs.RecordSlot("app", "dev", "slot2", 1, now) // CheckSlot 只读，需 RecordSlot 才记账
		if fs.CheckSlot("app", "dev", "slot2", slotPol, 1, now) {
			t.Fatal("second slot daily should block after record")
		}
	})

	t.Run("Budget", func(t *testing.T) {
		bc, err := budget.NewRedis(url, "", func(string, string, float64) {})
		if err != nil {
			t.Fatalf("new budget: %v", err)
		}
		defer bc.Close()
		if err := bc.Ping(ctx); err != nil {
			t.Fatalf("budget ping: %v", err)
		}
		bc.SyncBalances(map[string][2]float64{"adv1": {100, 0}})
		if !bc.TryDeduct("adv1", 30) {
			t.Fatal("deduct 30 of 100 should succeed")
		}
		if bc.TryDeduct("adv1", 80) {
			t.Fatal("deduct 80 (total 110>100) should fail")
		}
		if !bc.TryDeduct("adv1", 70) {
			t.Fatal("deduct 70 (total 100) should succeed")
		}
		spent, budgetLimit := bc.Stats("adv1")
		if budgetLimit != 100 || spent != 100 {
			t.Fatalf("stats mismatch: spent=%v budget=%v", spent, budgetLimit)
		}
		// 跨实例共享：另起一个客户端读同一份状态
		bc2, err := budget.NewRedis(url, "", func(string, string, float64) {})
		if err != nil {
			t.Fatalf("new budget2: %v", err)
		}
		defer bc2.Close()
		spent2, _ := bc2.Stats("adv1")
		if spent2 != 100 {
			t.Fatalf("budget not shared across instances: spent2=%v", spent2)
		}
	})

	t.Run("Queue", func(t *testing.T) {
		q, err := queue.NewRedis(url, "", "adcenter-test", 500)
		if err != nil {
			t.Fatalf("new queue: %v", err)
		}
		defer q.Close()
		ev := store.AdEvent{
			AppID: "app1", Style: "rewarded_video", AdvertiserID: "adv1",
			CreativeID: "cr1", DeviceID: "dev1", EventType: "impression", Revenue: 1.5,
		}
		q.Publish(ev)
		got := make(chan store.AdEvent, 1)
		qctx, cancel := context.WithCancel(ctx)
		defer cancel()
		go func() {
			q.Run(qctx, func(ctx context.Context, batch []store.AdEvent) error {
				for _, e := range batch {
					got <- e
				}
				return nil
			})
		}()
		select {
		case e := <-got:
			if e.AppID != "app1" || e.Revenue != 1.5 || e.EventType != "impression" {
				t.Fatalf("event mismatch: %+v", e)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("queue consume timeout")
		}
		// 验证 stream 落在 DB10
		n, err := raw.Exists(ctx, "events").Result()
		if err != nil || n != 1 {
			t.Fatalf("stream not in DB10: n=%d err=%v", n, err)
		}
	})
}
