package frequency

import (
	"testing"
	"time"
)

// runStoreContract 是频控接口的契约测试套件（ARCHITECTURE.md §5.3.1）。
// P0 内存实现与 P1 Redis 实现跑同一套用例；Redis 实现跑不过不许上线。
//
// 注意：各机制（多窗口/疲劳/隔离）的用例用独立策略隔离验证，组合语义
// 单独成组——避免机制间互相干扰导致用例意图模糊。
func runStoreContract(t *testing.T, s Store) {
	base := time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC)
	const app, dev, slot, advA, advB = "app1", "dev1", "slot1", "advA", "advB"

	slotPolicy := SlotPolicy{Interval: 20 * time.Minute, DailyLimit: 8}
	comboWindows := []Window{{WindowMinutes: 180, MaxCount: 3}, {WindowMinutes: 1440, MaxCount: 10}}
	fatiguePolicy := AdvPolicy{FatigueN: 3} // 仅疲劳窗口，无广告主窗口

	t.Run("首次请求无任何记录", func(t *testing.T) {
		if !s.CheckSlot(app, dev, slot, slotPolicy, 3, base) {
			t.Fatal("首次 CheckSlot 应通过")
		}
		if !s.CheckAndIncr(app, dev, slot, advA, fatiguePolicy, base) {
			t.Fatal("首次 CheckAndIncr 应通过")
		}
	})

	t.Run("展示间隔内拒绝", func(t *testing.T) {
		s.RecordSlot(app, dev, slot, 1, base)
		if s.CheckSlot(app, dev, slot, slotPolicy, 1, base.Add(10*time.Minute)) {
			t.Fatal("间隔 20min 内应拒绝")
		}
	})

	t.Run("间隔滑出后放行", func(t *testing.T) {
		if !s.CheckSlot(app, dev, slot, slotPolicy, 1, base.Add(21*time.Minute)) {
			t.Fatal("间隔滑出后应放行")
		}
	})

	t.Run("批量count计入剩余额度", func(t *testing.T) {
		const dev = "dev2"
		s.RecordSlot(app, dev, slot, 6, base)
		if s.CheckSlot(app, dev, slot, slotPolicy, 3, base.Add(21*time.Minute)) {
			t.Fatal("剩余额度 2 不够 count=3，应拒绝")
		}
		if !s.CheckSlot(app, dev, slot, slotPolicy, 2, base.Add(21*time.Minute)) {
			t.Fatal("剩余额度 2 应恰好放行 count=2")
		}
	})

	t.Run("广告主3h窗口第4次拒绝_滑出后放行", func(t *testing.T) {
		const dev = "dev3"
		policy := AdvPolicy{Windows: []Window{{WindowMinutes: 180, MaxCount: 3}}}
		for i := range 3 {
			if !s.CheckAndIncr(app, dev, slot, advA, policy, base.Add(time.Duration(i)*time.Minute)) {
				t.Fatalf("第 %d 次应通过", i+1)
			}
		}
		if s.CheckAndIncr(app, dev, slot, advA, policy, base.Add(3*time.Minute)) {
			t.Fatal("3h/3 档第 4 次应拒绝")
		}
		// 推进到窗口完全滑出（最后一次填充 +3h 之后）
		if !s.CheckAndIncr(app, dev, slot, advA, policy, base.Add(3*time.Hour+3*time.Minute)) {
			t.Fatal("3h 窗口滑出后应放行")
		}
	})

	t.Run("24h档独立生效", func(t *testing.T) {
		const dev = "dev4"
		policy := AdvPolicy{Windows: []Window{{WindowMinutes: 1440, MaxCount: 4}}}
		for i := range 4 {
			if !s.CheckAndIncr(app, dev, slot, advA, policy, base.Add(time.Duration(i)*time.Minute)) {
				t.Fatalf("第 %d 次应通过", i+1)
			}
		}
		if s.CheckAndIncr(app, dev, slot, advA, policy, base.Add(10*time.Minute)) {
			t.Fatal("24h/4 档第 5 次应拒绝")
		}
		if !s.CheckAndIncr(app, dev, slot, advA, policy, base.Add(24*time.Hour+5*time.Minute)) {
			t.Fatal("24h 窗口滑出后应放行")
		}
	})

	t.Run("多档组合_3h滑出但24h继续计数", func(t *testing.T) {
		const dev = "dev5"
		policy := AdvPolicy{Windows: comboWindows}
		// 先用满 3h/3
		for i := range 3 {
			if !s.CheckAndIncr(app, dev, slot, advA, policy, base.Add(time.Duration(i)*time.Minute)) {
				t.Fatalf("第 %d 次应通过", i+1)
			}
		}
		if s.CheckAndIncr(app, dev, slot, advA, policy, base.Add(3*time.Minute)) {
			t.Fatal("3h 档第 4 次应拒绝")
		}
		// +3h：3h 档只剩 2 条在窗，放行；24h 档累计到 4
		if !s.CheckAndIncr(app, dev, slot, advA, policy, base.Add(3*time.Hour+3*time.Minute)) {
			t.Fatal("3h 档滑出后应放行（24h 档尚有余额）")
		}
		// 以 ≥3h 间隔填满到 10（每个检查点 3h 窗口内 ≤2 条），
		// 最后一次填充在 base+22h
		for i := range 6 {
			at := base.Add(7*time.Hour + time.Duration(i)*3*time.Hour)
			if !s.CheckAndIncr(app, dev, slot, advA, policy, at) {
				t.Fatalf("累计第 %d 次应通过", i+5)
			}
		}
		// base+22h 后 1 分钟：24h 窗口覆盖全部 10 次填充 → 第 11 次拒绝
		if s.CheckAndIncr(app, dev, slot, advA, policy, base.Add(22*time.Hour+time.Minute)) {
			t.Fatal("24h/10 档第 11 次应拒绝")
		}
	})

	t.Run("疲劳窗口连续N次不重复", func(t *testing.T) {
		const dev = "dev6"
		now := base
		s.CheckAndIncr(app, dev, slot, advA, fatiguePolicy, now)
		s.CheckAndIncr(app, dev, slot, advB, fatiguePolicy, now)
		if s.CheckAndIncr(app, dev, slot, advA, fatiguePolicy, now) {
			t.Fatal("recent=[A,B]，A 应被疲劳窗口拒绝")
		}
		if s.CheckAndIncr(app, dev, slot, advB, fatiguePolicy, now) {
			t.Fatal("recent=[A,B]，B 应被疲劳窗口拒绝")
		}
		if !s.CheckAndIncr(app, dev, slot, "advC", fatiguePolicy, now) {
			t.Fatal("新广告主应通过")
		}
		// recent=[A,B,C]：A 仍在窗口 3 内
		if s.CheckAndIncr(app, dev, slot, advA, fatiguePolicy, now) {
			t.Fatal("A 仍在最近 3 次内应拒绝")
		}
		// 填入 D 后 recent=[B,C,D]：A 滑出
		s.CheckAndIncr(app, dev, slot, "advD", fatiguePolicy, now)
		if !s.CheckAndIncr(app, dev, slot, advA, fatiguePolicy, now) {
			t.Fatal("A 已滑出疲劳窗口应通过")
		}
	})

	t.Run("疲劳窗口按广告位独立", func(t *testing.T) {
		const dev = "dev7"
		now := base
		s.CheckAndIncr(app, dev, slot, advA, fatiguePolicy, now)
		if !s.CheckAndIncr(app, dev, slot+"2", advA, fatiguePolicy, now) {
			t.Fatal("疲劳窗口应按广告位独立")
		}
	})

	t.Run("广告主窗口跨广告位共享", func(t *testing.T) {
		const dev = "dev8"
		policy := AdvPolicy{Windows: []Window{{WindowMinutes: 180, MaxCount: 3}}}
		for i := range 3 {
			if !s.CheckAndIncr(app, dev, slot, advA, policy, base.Add(time.Duration(i)*time.Minute)) {
				t.Fatalf("第 %d 次应通过", i+1)
			}
		}
		// 另一广告位：广告主级窗口全局共享，应拒绝
		if s.CheckAndIncr(app, dev, slot+"2", advA, policy, base.Add(5*time.Minute)) {
			t.Fatal("广告主级窗口应跨广告位共享")
		}
	})

	t.Run("跨App同deviceID隔离", func(t *testing.T) {
		const dev = "dev9"
		policy := AdvPolicy{Windows: []Window{{WindowMinutes: 180, MaxCount: 3}}}
		for i := range 3 {
			if !s.CheckAndIncr(app, dev, slot, advA, policy, base.Add(time.Duration(i)*time.Minute)) {
				t.Fatalf("第 %d 次应通过", i+1)
			}
		}
		if s.CheckAndIncr(app, dev, slot, advA, policy, base.Add(5*time.Minute)) {
			t.Fatal("app1 内应已用满")
		}
		if !s.CheckAndIncr("app2", dev, slot, advA, policy, base.Add(5*time.Minute)) {
			t.Fatal("不同 App 同 deviceID 应隔离")
		}
	})

	t.Run("检查失败不记账", func(t *testing.T) {
		const dev = "dev10"
		policy := AdvPolicy{Windows: []Window{{WindowMinutes: 180, MaxCount: 3}}}
		for i := range 3 {
			s.CheckAndIncr(app, dev, slot, advB, policy, base.Add(time.Duration(i)*time.Minute))
		}
		if s.CheckAndIncr(app, dev, slot, advB, policy, base.Add(5*time.Minute)) {
			t.Fatal("应拒绝")
		}
		if s.CheckAndIncr(app, dev, slot, advB, policy, base.Add(6*time.Minute)) {
			t.Fatal("失败不记账：仍应拒绝")
		}
		// 窗口完全滑出后恰好放行一次（若失败路径也记账，此处仍会被拒）
		if !s.CheckAndIncr(app, dev, slot, advB, policy, base.Add(3*time.Hour+3*time.Minute)) {
			t.Fatal("窗口滑出后应恰好放行（失败未记账的证明）")
		}
	})
}

func TestMemoryStoreContract(t *testing.T) {
	runStoreContract(t, NewMemory(24*time.Hour))
}
