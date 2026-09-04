package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"adcenter/internal/budget"
	"adcenter/internal/config"
	"adcenter/internal/engine"
	"adcenter/internal/frequency"
	"adcenter/internal/metrics"
)

// HTTP 决策路径基准（PLAN 1.9 的接口级验收：均值 <100ms / P99 <200ms
// 的服务端预算，本地压测应远低于此）。
//
// 完整链路：sha256 鉴权查快照 → JSON 解码 → 引擎决策 → metrics 计数
// → 事件入队（异步，不落库）→ JSON 编码。频控用直通替身（真实成本由
// frequency 包基准单独测量），预算/metrics/eventWriter 均为真实实现。

type benchFreqPassthrough struct{}

func (benchFreqPassthrough) CheckSlot(string, string, string, frequency.SlotPolicy, int, time.Time) bool {
	return true
}
func (benchFreqPassthrough) RecordSlot(string, string, string, int, time.Time) {}
func (benchFreqPassthrough) CheckAndIncr(string, string, string, string, frequency.AdvPolicy, time.Time) bool {
	return true
}

type fixedLoader struct{ snap *config.Snapshot }

func (l *fixedLoader) LoadSnapshot(context.Context) (*config.Snapshot, error) {
	return l.snap, nil
}

// benchAPIServer 构建带真实快照的 Server（20 候选典型规模，key 固定）。
func benchAPIServer(b *testing.B) (*Server, string) {
	const apiKey = "adc_bench_0000000000000000000000000001"
	sum := sha256.Sum256([]byte(apiKey))
	keyHash := hex.EncodeToString(sum[:])

	snap := &config.Snapshot{
		Apps:                  map[string]*config.App{},
		AppByKeyHash:          map[string]*config.App{},
		Advertisers:           map[string]*config.Advertiser{},
		Slots:                 map[string]*config.Slot{},
		SlotsByKey:            map[string]*config.Slot{},
		CreativesByAdvertiser: map[string][]*config.Creative{},
	}
	app := &config.App{ID: "app_bench", Status: "active"}
	snap.Apps[app.ID] = app
	snap.AppByKeyHash[keyHash] = app

	slot := &config.Slot{ID: "slot_bench", AppID: app.ID, Key: "bench", Status: "active",
		FreqDailyLimit: 1 << 20, FreqFatigueWindow: 3}
	snap.Slots[slot.ID] = slot
	snap.SlotsByKey[slot.Key] = slot

	bm := map[string][2]float64{}
	for i := range 20 {
		id := fmt.Sprintf("adv_%03d", i)
		snap.Advertisers[id] = &config.Advertiser{
			ID: id, Name: id, Tier: i%3 + 1, Status: "active",
			TargetCPI: 1 + float64(i%5)*0.2, ActualCPI: 0.3 + float64(i%7)*0.15,
			BiddingPrice: 0.5 + float64(i%9)*0.25, DailyBudget: 1e9,
		}
		bm[id] = [2]float64{1e9, 0}
		snap.CreativesByAdvertiser[id] = []*config.Creative{{
			ID: "cr_" + id, AdvertiserID: id, MediaType: "video", Status: "active", Weight: 1,
		}}
		share := 0.0
		if i == 0 {
			share = 0.3
		}
		slot.Priorities = append(slot.Priorities, config.FillPriority{
			ID: id, SourceType: "advertiser", AdvertiserID: id,
			GuaranteedShare: share, Weight: 1,
		})
	}

	discard := slog.New(slog.DiscardHandler)
	cache, err := config.NewCache(context.Background(), &fixedLoader{snap}, discard)
	if err != nil {
		b.Fatal(err)
	}
	bud := budget.NewMemory(bm, nil)
	s := &Server{
		Cache: cache, Engine: &engine.Engine{Freq: benchFreqPassthrough{}, Budget: bud},
		Metrics: metrics.New(), Budget: bud, Log: discard,
	}
	// 事件写入器：不启动 run 协程，队列满即丢弃（基准只测入队成本）
	s.SetEventWriter(newEventWriter(nil, discard))
	return s, apiKey
}

func benchmarkHTTPAdRequest(b *testing.B, count int) {
	s, apiKey := benchAPIServer(b)
	body := fmt.Sprintf(`{"slot":"bench","deviceId":"dev_bench","count":%d}`, count)

	b.ReportAllocs()
	for b.Loop() {
		r := httptest.NewRequest("POST", "/v1/ad/req", strings.NewReader(body))
		r.Header.Set("X-Api-Key", apiKey)
		w := httptest.NewRecorder()
		s.handleAdRequest(w, r)
		if w.Code != 200 {
			b.Fatalf("status %d: %s", w.Code, w.Body.String())
		}
	}
}

func BenchmarkHTTPAdRequest_Single(b *testing.B) { benchmarkHTTPAdRequest(b, 1) }
func BenchmarkHTTPAdRequest_Batch10(b *testing.B) {
	benchmarkHTTPAdRequest(b, 10)
}
