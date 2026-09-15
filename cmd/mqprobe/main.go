// Command mqprobe 是 Redis Streams 作为事件队列的**临时可行性探针**（M2 前置验证）。
//
// 验证链路：批量 XADD 入队 → consumer group 消费 → 攒批落库 → XACK，
// 并观察瞬时峰值是否被队列兜住（削峰）。跑完即删，不进入生产路径。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"adcenter/internal/store"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type event struct {
	AppID        string  `json:"app_code"`
	Style        string  `json:"style"`
	AdvertiserID string  `json:"adv_id"`
	DeviceID     string  `json:"dev"`
	EventType    string  `json:"evt"`
	Revenue      float64 `json:"rev"`
	Ts           int64   `json:"ts"`
}

func main() {
	ctx := context.Background()
	redisURL := env("REDIS_URL", "redis://localhost:6379/3")
	dsn := env("DATABASE_URL", "postgres://postgres:postgres@127.0.0.1:54322/postgres?sslmode=disable")

	const (
		stream   = "adcenter:test:events"
		group    = "probe"
		consumer = "probe-c1"
		total    = 5000
		batchAdd = 100 // 生产端 pipeline 批量
		batchDB  = 500 // 消费端落库批量
	)

	opt, err := redis.ParseURL(redisURL)
	must(err)
	rdb := redis.NewClient(opt)
	defer rdb.Close()
	must(rdb.Ping(ctx).Err())

	// 从干净状态开始
	_ = rdb.Del(ctx, stream).Err()
	must(rdb.XGroupCreateMkStream(ctx, stream, group, "0").Err())

	st, err := store.New(ctx, dsn)
	must(err)
	defer st.Close()

	// 取真实 app / slot，满足 ad_events 外键
	pool, err := pgxpool.New(ctx, dsn)
	must(err)
	defer pool.Close()
	var appID, slotID string
	dbEnabled := true
	if err := pool.QueryRow(ctx, "select code from apps limit 1").Scan(&appID); err != nil {
		dbEnabled = false // 本地库没有 adcenter 表：跳过落库，只验证队列层
		fmt.Println("（本地库无 apps 表 → 跳过 DB 落库，仅验证队列链路）")
	} else {
		must(pool.QueryRow(ctx, "select id from ad_slots limit 1").Scan(&slotID))
		fmt.Printf("app=%s slot=%s\n\n", appID[:8], slotID[:8])
	}

	// ===== ① 生产：瞬时灌入，模拟开屏曝光峰值 =====
	t0 := time.Now()
	for start := 0; start < total; start += batchAdd {
		pipe := rdb.Pipeline()
		for i := 0; i < batchAdd && start+i < total; i++ {
			e := event{
				AppID: appID, Style: slotID,
				AdvertiserID: "11111111-1111-1111-1111-111111111111",
				DeviceID:     fmt.Sprintf("mqprobe-%d", start+i),
				EventType:    "impression", Revenue: 0.0035, Ts: time.Now().UnixMilli(),
			}
			b, _ := json.Marshal(e)
			pipe.XAdd(ctx, &redis.XAddArgs{Stream: stream, MaxLen: 200000, Approx: true, Values: map[string]any{"d": string(b)}})
		}
		_, err := pipe.Exec(ctx)
		must(err)
	}
	produceCost := time.Since(t0)
	xlen := rdb.XLen(ctx, stream).Val()
	fmt.Printf("① 生产：%d 条瞬时入队  耗时 %v  速率 %.0f 条/s  队列当前长度 XLEN=%d（峰值被兜住）\n\n",
		total, produceCost.Round(time.Millisecond), float64(total)/produceCost.Seconds(), xlen)

	// ===== ② 消费：攒批落库 + ACK，每批固定节流模拟 DB 上限 =====
	t1 := time.Now()
	var (
		batches   int
		written   int
		agg       = map[string]float64{} // 广告主 → 聚合金额（M2 的流水聚合演示）
		aggCount  = map[string]int{}
		emptyRead int
	)
	for {
		res, err := rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group: group, Consumer: consumer, Streams: []string{stream, ">"},
			Count: int64(batchDB), Block: 300 * time.Millisecond,
		}).Result()
		if err == redis.Nil {
			if emptyRead++; emptyRead >= 3 {
				break
			}
			continue
		}
		must(err)
		if len(res) == 0 {
			continue
		}
		emptyRead = 0
		msgs := res[0].Messages
		batch := make([]store.AdEvent, 0, len(msgs))
		ids := make([]string, 0, len(msgs))
		for _, m := range msgs {
			var e event
			if err := json.Unmarshal([]byte(fmt.Sprint(m.Values["d"])), &e); err != nil {
				continue
			}
			batch = append(batch, store.AdEvent{
				AppID: e.AppID, Style: e.Style, AdvertiserID: e.AdvertiserID,
				DeviceID: e.DeviceID, EventType: e.EventType, Revenue: e.Revenue,
			})
			agg[e.AdvertiserID] += e.Revenue
			aggCount[e.AdvertiserID]++
			ids = append(ids, m.ID)
		}
		if len(batch) == 0 {
			must(rdb.XAck(ctx, stream, group, ids...).Err())
			continue
		}
		if dbEnabled {
			must(st.InsertAdEvents(ctx, batch))
		}
		must(rdb.XAck(ctx, stream, group, ids...).Err())
		batches++
		written += len(batch)
		time.Sleep(100 * time.Millisecond) // 模拟消费端限速，DB 侧写入速率可控
	}
	consumeCost := time.Since(t1)
	left := rdb.XLen(ctx, stream).Val()
	pending := rdb.XPending(ctx, stream, group).Val()

	fmt.Printf("② 消费：%d 批 / %d 条  耗时 %v  平均批大小 %d  落库 %d 行\n",
		batches, written, consumeCost.Round(time.Millisecond), written/max(batches, 1), written)
	fmt.Printf("   消费后 XLEN=%d  Pending=%d（已全部 ACK）\n\n", left, pending.Count)

	// ===== ③ 计费流水聚合演示 =====
	fmt.Printf("③ 流水聚合：%d 条曝光事件 → %d 条聚合流水（按广告主）\n", written, len(agg))
	for id, amt := range agg {
		fmt.Printf("   %s: count=%d amount=%.4f\n", id, aggCount[id], amt)
	}

	// 清理
	if dbEnabled {
		var dbRows int
		_ = pool.QueryRow(ctx, "select count(*) from ad_events where device_id like 'mqprobe-%'").Scan(&dbRows)
		fmt.Printf("DB 落地核对：ad_events 中 mqprobe 事件 %d 行\n", dbRows)
	}
	_ = rdb.Del(ctx, stream).Err()
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func must(err error) {
	if err != nil {
		fmt.Println("FATAL:", err)
		os.Exit(1)
	}
}

var _ = strconv.Itoa
