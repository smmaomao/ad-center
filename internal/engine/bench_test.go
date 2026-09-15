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
// 频控用直通替身隔离引擎纯计算成本；预算用真实 Memory 实现。

type benchFreq struct{}

func (benchFreq) CheckSlot(string, string, string, frequency.SlotPolicy, int, time.Time) bool {
	return true
}
func (benchFreq) RecordSlot(string, string, string, int, time.Time) {}
func (benchFreq) CheckAndIncr(string, string, string, string, frequency.AdvPolicy, time.Time) bool {
	return true
}

// benchSnapshot 构建基准快照：nAdv 个活跃广告主（tier/出价/达成率错开制造真实排序压力），
// 每广告主 5 个活跃素材（均支持 rewarded_video、投放全部 App）。
func benchSnapshot(nAdv int) *config.Snapshot {
	snap := &config.Snapshot{
		Apps:                  map[string]*config.App{"app_bench": {ID: "app_bench", Status: "active"}},
		Advertisers:           map[string]*config.Advertiser{},
		CreativesByAdvertiser: map[string][]*config.Creative{},
		Campaigns:             map[string]*config.Campaign{},
		CreativeCampaign:      map[string]string{},
		PricingBenchmark:      config.DefaultPricingBenchmark(),
	}
	for i := range nAdv {
		id := fmt.Sprintf("adv_%03d", i)
		snap.Advertisers[id] = &config.Advertiser{
			ID: id, Name: id, Status: "active",
		}
		crs := make([]*config.Creative, 5)
		for j := range crs {
			crs[j] = &config.Creative{
				ID: fmt.Sprintf("cr_%s_%d", id, j), AdvertiserID: id,
				MediaType: "video", Status: "active",
				Styles:      []string{"rewarded_video"},
			}
		}
		snap.CreativesByAdvertiser[id] = crs
		// KPI 现属 campaign：每个广告主一个 campaign，素材归属之，制造排序压力。
		campID := "cmp_" + id
		camp := &config.Campaign{
			ID: campID, AdvertiserID: id, Status: "active", DailyBudget: 1e9,
			TargetCPI: 1 + float64(i%5)*0.2, ActualCPI: 0.3 + float64(i%7)*0.15,
			BillingMode: "cpm", BiddingPrice: 15,
			CreativeIDs: []string{},
		}
		for _, cr := range crs {
			camp.CreativeIDs = append(camp.CreativeIDs, cr.ID)
			snap.CreativeCampaign[cr.ID] = campID
		}
		snap.Campaigns[campID] = camp
	}
	return snap
}

func benchmarkDecide(b *testing.B, nAdv, count int) {
	snap := benchSnapshot(nAdv)
	e := &Engine{Freq: benchFreq{}, Budget: budget.NewMemory(map[string][2]float64{}, nil)}
	req := Request{App: snap.Apps["app_bench"], Style: "rewarded_video",
		DeviceID: "bench_dev", Count: count, Now: testNow}

	b.ReportAllocs()
	for b.Loop() {
		resp := e.Decide(snap, req)
		if len(resp.Items) == 0 {
			b.Fatal("expected items, got fallback")
		}
	}
}

// 典型规模：单条决策（100 个候选，一个 App 的常态配置）。
func BenchmarkDecideSingle_20cands(b *testing.B) { benchmarkDecide(b, 20, 1) }

// 极端规模：单条决策（200 个候选）。
func BenchmarkDecideSingle_200cands(b *testing.B) { benchmarkDecide(b, 200, 1) }

// 批量预取（PRD 5.5）：count=5 / count=20（上限）。
func BenchmarkDecideBatch5_20cands(b *testing.B)  { benchmarkDecide(b, 20, 5) }
func BenchmarkDecideBatch20_20cands(b *testing.B) { benchmarkDecide(b, 20, 20) }

// 最重路径：批量上限 × 极端候选数。
func BenchmarkDecideBatch20_200cands(b *testing.B) { benchmarkDecide(b, 200, 20) }
