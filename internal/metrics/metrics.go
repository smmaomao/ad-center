// Package metrics 提供实时指标聚合：内存计数器（slot×advertiser×分钟粒度），
// SSE 推送给监控看板（P0.7），每分钟批量落库 metrics_minute（UPSERT 累加）。
package metrics

import (
	"context"
	"sync"
	"time"

	"adcenter/internal/store"
)

// zeroAdvertiser 兜底/MAX 聚合的哨兵。advertiser_id 已迁移为 bigint 且 NOT NULL，
// 用 0 作为跨广告主聚合行的占位（真实广告主 id 从序列 1 起，0 永不冲突）。
const zeroAdvertiser = "0"

type key struct {
	app, style, adv string
	minute          time.Time
}

type counters struct {
	requests, fills, impressions, clicks, conversions int64
	revenue                                           float64
}

// Agg 并发安全的分钟级计数器。
type Agg struct {
	mu sync.Mutex
	m  map[key]*counters
}

// New 创建聚合器。
func New() *Agg { return &Agg{m: map[key]*counters{}} }

func (a *Agg) k(app, style, adv string, now time.Time) key {
	if adv == "" {
		adv = zeroAdvertiser
	}
	return key{app, style, adv, now.UTC().Truncate(time.Minute)}
}

// Record 记录一次请求的结果（items>0 为直售填充，否则为兜底）。
// V1.2 起 revenue 恒为 0：req 只下发不产生收入，收入在计费事件
// （RecordEvent）累计。
func (a *Agg) Record(app, style, adv string, items int, revenue float64, now time.Time) {
	k := a.k(app, style, adv, now)
	a.mu.Lock()
	defer a.mu.Unlock()
	c := a.m[k]
	if c == nil {
		c = &counters{}
		a.m[k] = c
	}
	c.requests++
	c.fills += int64(items)
	c.revenue += revenue
}

// RecordEvent 记录回执事件并累计收入（revenue = 服务端确认的实际扣费：
// cpm impression / cpc click / cpa S2S 转化；其余事件恒 0）。
func (a *Agg) RecordEvent(app, style, adv string, event string, revenue float64, now time.Time) {
	k := a.k(app, style, adv, now)
	a.mu.Lock()
	defer a.mu.Unlock()
	c := a.m[k]
	if c == nil {
		c = &counters{}
		a.m[k] = c
	}
	switch event {
	case "impression":
		c.impressions++
	case "click":
		c.clicks++
	// V1.2：转化计数包含 S2S 细分事件（install/activate/register/
	// first_purchase/purchase）与 V1.1 遗留的 conversion（客户端通道已拒绝该
	// 事件，历史数据兼容）。
	case "conversion", "install", "activate", "register", "first_purchase", "purchase":
		c.conversions++
	}
	c.revenue += revenue
}

// Swap 取出并清空全部计数（落库用）。
func (a *Agg) Swap() map[key]*counters {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := a.m
	a.m = map[key]*counters{}
	return out
}

// Flush 将计数批量 UPSERT 到 metrics_minute。
func (a *Agg) Flush(ctx context.Context, s *store.Store) error {
	snapshot := a.Swap()
	if len(snapshot) == 0 {
		return nil
	}
	rows := make([]store.MinuteMetric, 0, len(snapshot))
	for k, c := range snapshot {
		rows = append(rows, store.MinuteMetric{
			AppID: k.app, Style: k.style, AdvertiserID: k.adv, Minute: k.minute,
			Requests: c.requests, Fills: c.fills, Impressions: c.impressions,
			Clicks: c.clicks, Conversions: c.conversions, Revenue: c.revenue,
		})
	}
	return s.FlushMinuteMetrics(ctx, rows)
}

// RunFlushLoop 每分钟落库（含重试：失败计数并回 next 轮——UPSERT 累加语义天然可重试）。
func (a *Agg) RunFlushLoop(ctx context.Context, s *store.Store, log interface{ Error(string, ...any) }) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			// 退出前尽力落最后一轮
			if err := a.Flush(context.Background(), s); err != nil {
				log.Error("metrics final flush failed", "err", err)
			}
			return
		case <-t.C:
			if err := a.Flush(ctx, s); err != nil {
				log.Error("metrics flush failed", "err", err)
			}
		}
	}
}

// Size 当前计数条数（监控用）。
func (a *Agg) Size() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.m)
}
