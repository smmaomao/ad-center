package budget

import (
	"testing"
	"time"
)

// runCtrlContract 是预算控制接口的契约测试套件（ARCHITECTURE.md §5.3.1）。
// P0 内存实现与 P1 Redis 实现跑同一套用例。
func runCtrlContract(t *testing.T, c Ctrl, setNow func(time.Time)) {
	base := time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC) // Jakarta 17:00
	const adv = "adv1"

	t.Run("余额充足扣减成功", func(t *testing.T) {
		if !c.TryDeduct(adv, 10) {
			t.Fatal("余额 100 扣 10 应成功")
		}
		spent, budget := c.Stats(adv)
		if spent != 10 || budget != 100 {
			t.Fatalf("Stats = (%v,%v)，期望 (10,100)", spent, budget)
		}
	})

	t.Run("余额不足拒绝且状态不变", func(t *testing.T) {
		if c.TryDeduct(adv, 95) {
			t.Fatal("余额 90 扣 95 应拒绝")
		}
		if spent, _ := c.Stats(adv); spent != 10 {
			t.Fatalf("拒绝后 spent 应保持 10，实际 %v", spent)
		}
	})

	t.Run("预算仅够一次时单响应最多一次", func(t *testing.T) {
		const adv = "adv2" // 预算 100，此前已扣 95 → 余 5
		if !c.TryDeduct(adv, 5) {
			t.Fatal("余额 5 扣 5 应成功")
		}
		if c.TryDeduct(adv, 5) {
			t.Fatal("余额 0，第二次应拒绝（单响应最多出现一次）")
		}
	})

	t.Run("未知广告主拒绝", func(t *testing.T) {
		if c.TryDeduct("ghost", 1) {
			t.Fatal("未注册广告主应拒绝")
		}
		if spent, budget := c.Stats("ghost"); spent != 0 || budget != 0 {
			t.Fatal("未知广告主 Stats 应返回 (0,0)")
		}
	})

	t.Run("Rollback返还额度后可再扣", func(t *testing.T) {
		const adv = "adv3" // 预算 50
		if !c.TryDeduct(adv, 40) {
			t.Fatal("预扣 40 应成功")
		}
		if c.TryDeduct(adv, 20) {
			t.Fatal("余 10 扣 20 应拒绝")
		}
		if err := c.Rollback(adv, 40); err != nil {
			t.Fatalf("Rollback 出错: %v", err)
		}
		if !c.TryDeduct(adv, 20) {
			t.Fatal("回滚后余 50，扣 20 应成功")
		}
	})

	t.Run("Commit不改变余额", func(t *testing.T) {
		const adv = "adv4" // 预算 30
		if !c.TryDeduct(adv, 10) {
			t.Fatal("预扣应成功")
		}
		if err := c.Commit(adv, 10); err != nil {
			t.Fatalf("Commit 出错: %v", err)
		}
		if spent, _ := c.Stats(adv); spent != 10 {
			t.Fatalf("Commit 后 spent 应保持 10（预扣时已扣），实际 %v", spent)
		}
	})

	t.Run("跨日重置_Jakarta零点", func(t *testing.T) {
		const adv = "adv5" // 预算 20
		if !c.TryDeduct(adv, 15) {
			t.Fatal("预扣应成功")
		}
		// 推进到 Jakarta 次日（base = Jakarta 17:00，+8h 即次日 01:00）
		setNow(base.Add(8 * time.Hour))
		if !c.TryDeduct(adv, 20) {
			t.Fatal("跨日重置后预算应恢复满额")
		}
		if spent, _ := c.Stats(adv); spent != 20 {
			t.Fatalf("跨日后已扣 20，实际 %v", spent)
		}
		setNow(base) // 恢复时钟，不污染后续用例
	})

	t.Run("零金额扣减", func(t *testing.T) {
		const adv = "adv6" // 预算 10（前置用例的跨日重置可能已清空，自行构造耗尽态）
		for range 10 {
			if !c.TryDeduct(adv, 1) {
				t.Fatal("逐次扣 1 应全部成功")
			}
		}
		if c.TryDeduct(adv, 1) {
			t.Fatal("预算耗尽应拒绝")
		}
		if !c.TryDeduct(adv, 0) {
			t.Fatal("零金额扣减应放行（不消耗额度）")
		}
	})
}

func TestMemoryCtrlContract(t *testing.T) {
	now := time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC)
	ctrl := NewMemory(map[string][2]float64{
		"adv1": {100, 0},
		"adv2": {100, 95},
		"adv3": {50, 0},
		"adv4": {30, 0},
		"adv5": {20, 0},
		"adv6": {10, 10},
	}, nil)
	ctrl.SetNow(func() time.Time { return now })
	runCtrlContract(t, ctrl, func(t time.Time) { now = t })
}
