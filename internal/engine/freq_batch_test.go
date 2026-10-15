package engine

import (
	"testing"
	"time"

	"adcenter/internal/config"
	"adcenter/internal/frequency"
)

// batchFreq 频控替身：实现 BatchChecker，并统计批量 / 逐条调用次数。
// 嵌入 fakeFreq 只为复用其余接口方法（Check 与 CheckBatch 下面各自覆盖）。
type batchFreq struct {
	fakeFreq
	batches int
	checks  int
	deny    map[string]bool // campaignID → 频控不放行
	fail    bool            // 模拟批量失败（返回 nil）
}

func (f *batchFreq) Check(_, _, _, advID string, _ frequency.AdvPolicy, _ time.Time) bool {
	f.checks++
	return !f.deny[advID]
}

func (f *batchFreq) CheckBatch(_, _ string, items []frequency.CheckItem, _ time.Time) []bool {
	f.batches++
	if f.fail {
		return nil
	}
	out := make([]bool, len(items))
	for i, it := range items {
		out[i] = !f.deny[it.AdvertiserID]
	}
	return out
}

// freqSnapshot 三个任务各挂一个素材，全部开启频控窗口（否则不进批量入参）。
func freqSnapshot() *config.Snapshot {
	snap := mkSnapshot(mkAdv("a1", 1, 1, 1), mkAdv("a2", 2, 1, 1), mkAdv("a3", 3, 1, 1))
	for _, c := range snap.Campaigns {
		c.FreqIntervalMinute = 20
		c.FreqFatigueWindow = 3
		c.FreqDailyLimit = 100
	}
	return snap
}

// TestDecide_FreqBatchOneRoundTrip 决策路径的频控只读检查必须**一次批量往返**完成，
// 不再逐候选一条命令（Redis 用量最大单项，见 frequency.BatchChecker 注释）。
func TestDecide_FreqBatchOneRoundTrip(t *testing.T) {
	snap := freqSnapshot()
	f := &batchFreq{deny: map[string]bool{"cmp_a2": true}}
	e := &Engine{Freq: f, Budget: mkBudget(), Pick: func(n int) int { return 0 }}
	req := Request{App: snap.Apps["app1"], Style: "rewarded_video",
		DeviceID: "d1", Count: 3, Now: testNow}

	resp := e.Decide(snap, req)
	if f.batches != 1 {
		t.Fatalf("批量频控检查应只调用 1 次，实际 %d", f.batches)
	}
	if f.checks != 0 {
		t.Fatalf("走批量后不应再逐条 Check，实际 %d 次", f.checks)
	}
	got := ids(resp.Items)
	for _, id := range got {
		if id == "a2" {
			t.Fatalf("被频控挡住的 a2 不应下发，实际 %v", got)
		}
	}
	if len(got) != 2 {
		t.Fatalf("应下发 a1/a3 两条，实际 %v", got)
	}
}

// TestDecide_FreqBatchFallback 批量失败（返回 nil）必须回退逐条 Check，语义不变。
func TestDecide_FreqBatchFallback(t *testing.T) {
	snap := freqSnapshot()
	f := &batchFreq{deny: map[string]bool{"cmp_a2": true}, fail: true}
	e := &Engine{Freq: f, Budget: mkBudget(), Pick: func(n int) int { return 0 }}
	req := Request{App: snap.Apps["app1"], Style: "rewarded_video",
		DeviceID: "d1", Count: 3, Now: testNow}

	resp := e.Decide(snap, req)
	if f.batches != 1 {
		t.Fatalf("应尝试批量 1 次，实际 %d", f.batches)
	}
	if f.checks == 0 {
		t.Fatal("批量失败后应逐条回退 Check")
	}
	for _, id := range ids(resp.Items) {
		if id == "a2" {
			t.Fatalf("回退后 a2 仍应被挡，实际 %v", ids(resp.Items))
		}
	}
}
