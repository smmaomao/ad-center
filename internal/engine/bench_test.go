package engine

import (
	"fmt"
	"testing"
	"time"

	"adcenter/internal/budget"
	"adcenter/internal/config"
	"adcenter/internal/frequency"
)

// 基准测试（PLAN 1.9，目标：引擎耗时 <10ms）。
//
// 频控用直通替身隔离引擎纯计算成本（频控/预算存储的真实成本由
// frequency 包基准单独测量——生产稳态下其状态受窗口 max 上限约束，
// 与此处解耦测量互不污染）。预算用真实 Memory 实现（成本恒定无状态增长）。

type benchFreq struct{}

func (benchFreq) CheckSlot(string, string, string, frequency.SlotPolicy, int, time.Time) bool {
	return true
}
func (benchFreq) RecordSlot(string, string, string, int, time.Time) {}
func (benchFreq) CheckAndIncr(string, string, string, string, frequency.AdvPolicy, time.Time) bool {
	return true
}

// benchSnapshot 构建基准快照：nAdv 个活跃广告主（tier/出价/达成率错开制造
// 真实排序压力），每广告主 5 个活跃素材，第 0 个候选带 30% 保底份额。
func benchSnapshot(nAdv int) (*config.Snapshot, *config.Slot, map[string][2]float64) {
	snap := &config.Snapshot{
		Apps:                  map[string]*config.App{"app_bench": {ID: "app_bench", Status: "active"}},
		Advertisers:           map[string]*config.Advertiser{},
		Slots:                 map[string]*config.Slot{},
		SlotsByKey:            map[string]*config.Slot{},
		CreativesByAdvertiser: map[string][]*config.Creative{},
	}
	slot := &config.Slot{ID: "slot_bench", AppID: "app_bench", Key: "bench", Status: "active",
		FreqDailyLimit: 1 << 20, FreqIntervalMinutes: 0, FreqFatigueWindow: 3}
	snap.Slots[slot.ID] = slot
	snap.SlotsByKey[slot.Key] = slot

	bm := map[string][2]float64{}
	for i := range nAdv {
		id := fmt.Sprintf("adv_%03d", i)
		snap.Advertisers[id] = &config.Advertiser{
			ID: id, Name: id, Tier: i%3 + 1, Status: "active",
			TargetCPI:    1 + float64(i%5)*0.2,
			ActualCPI:    0.3 + float64(i%7)*0.15,
			BiddingPrice: 0.5 + float64(i%9)*0.25,
			DailyBudget:  1e9, // 基准中不触顶（扣减累加恒定成本）
		}
		bm[id] = [2]float64{1e9, 0}
		crs := make([]*config.Creative, 5)
		for j := range crs {
			crs[j] = &config.Creative{
				ID: fmt.Sprintf("cr_%s_%d", id, j), AdvertiserID: id,
				MediaType: "video", Status: "active", Weight: 1 + float64(j%3),
			}
		}
		snap.CreativesByAdvertiser[id] = crs

		share := 0.0
		if i == 0 {
			share = 0.3 // 保底候选
		}
		slot.Priorities = append(slot.Priorities, config.FillPriority{
			ID: id, SourceType: "advertiser", AdvertiserID: id,
			GuaranteedShare: share, Weight: 1,
		})
	}
	return snap, slot, bm
}

func benchmarkDecide(b *testing.B, nAdv, count int) {
	snap, slot, bm := benchSnapshot(nAdv)
	e := &Engine{Freq: benchFreq{}, Budget: budget.NewMemory(bm, nil)}
	req := Request{App: snap.Apps["app_bench"], Slot: slot,
		DeviceID: "bench_dev", Count: count, Now: testNow}

	b.ReportAllocs()
	for b.Loop() {
		resp := e.Decide(snap, req)
		if len(resp.Items) == 0 {
			b.Fatal("expected items, got fallback")
		}
	}
}

// 典型规模：单条决策（20 个候选，一个广告位的常态配置）。
func BenchmarkDecideSingle_20cands(b *testing.B) { benchmarkDecide(b, 20, 1) }

// 极端规模：单条决策（200 个候选挂同一广告位）。
func BenchmarkDecideSingle_200cands(b *testing.B) { benchmarkDecide(b, 200, 1) }

// 批量预取（PRD 5.5）：count=5 / count=20（上限）。
func BenchmarkDecideBatch5_20cands(b *testing.B)  { benchmarkDecide(b, 20, 5) }
func BenchmarkDecideBatch20_20cands(b *testing.B) { benchmarkDecide(b, 20, 20) }

// 最重路径：批量上限 × 极端候选数（保底换入需全表扫描）。
func BenchmarkDecideBatch20_200cands(b *testing.B) { benchmarkDecide(b, 200, 20) }
