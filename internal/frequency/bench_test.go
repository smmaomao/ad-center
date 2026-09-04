package frequency

import (
	"testing"
	"time"
)

// MemoryStore 稳态基准（决策路径的真实频控成本）。
//
// 生产稳态：每 (app,device,advertiser) 的 fills 受窗口 max 上限约束
//（如 24h/10），状态规模恒定。此处预填至触顶，测"检查→拒绝"路径
//（不写状态，成本恒定可重复）；通过路径仅多一次 append+裁剪。

func BenchmarkCheckAndIncr_Window10_Topped(b *testing.B) {
	m := NewMemory(24 * time.Hour)
	policy := AdvPolicy{Windows: []Window{{WindowMinutes: 1440, MaxCount: 10}}}
	now := time.Now()
	// 预填 10 条触顶（拒绝后不再变化 = 稳态）
	for i := range 10 {
		m.CheckAndIncr("app", "dev", "slot", "adv", policy, now.Add(-time.Duration(i)*time.Minute))
	}
	b.ReportAllocs()
	for b.Loop() {
		if m.CheckAndIncr("app", "dev", "slot", "adv", policy, now) {
			b.Fatal("should be rejected")
		}
	}
}

// 多窗口（3h/3 + 24h/10）+ 疲劳窗口的组合检查成本。
func BenchmarkCheckAndIncr_MultiWindowFatigue_Topped(b *testing.B) {
	m := NewMemory(24 * time.Hour)
	policy := AdvPolicy{
		Windows:  []Window{{WindowMinutes: 180, MaxCount: 3}, {WindowMinutes: 1440, MaxCount: 10}},
		FatigueN: 3,
	}
	now := time.Now()
	// 10 条全在疲劳窗口内且触顶（最近 3 条含同一广告主 → 疲劳先拒）
	for i := range 10 {
		m.CheckAndIncr("app", "dev", "slot", "adv", policy, now.Add(-time.Duration(i)*time.Minute))
	}
	b.ReportAllocs()
	for b.Loop() {
		if m.CheckAndIncr("app", "dev", "slot", "adv", policy, now) {
			b.Fatal("should be rejected")
		}
	}
}

// CheckSlot 稳态（日频控检查，fills 固定 10 条）。
func BenchmarkCheckSlot_DailyLimit(b *testing.B) {
	m := NewMemory(24 * time.Hour)
	policy := SlotPolicy{Interval: 20 * time.Minute, DailyLimit: 10}
	now := time.Now()
	m.RecordSlot("app", "dev", "slot", 10, now.Add(-time.Minute))
	b.ReportAllocs()
	for b.Loop() {
		if m.CheckSlot("app", "dev", "slot", policy, 1, now) {
			b.Fatal("should be rejected")
		}
	}
}
