package budget

import (
	"testing"
	"time"
)

// runCtrlContract 是预算控制接口的契约测试套件（ARCHITECTURE.md §5.3.1）。
// P0 内存实现与 P1 Redis 实现跑同一套用例。
//
// 计费模型（migration 000008）：TryDeduct 是"事件确认扣费"——只在真实计费
// 事件（impression/click/S2S 转化）到达时调用；req 路径只读不扣（见
// engine.materialize 的只读闸，靠 Stats 判断 spent >= budget）。
//
// sync 为可选：传入后额外验证"配置热更新 → 预算同步"（B2/B3）的契约。
func runCtrlContract(t *testing.T, c Ctrl, setNow func(time.Time), sync func(all map[string][2]float64)) {
	base := time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC) // Jakarta 17:00
	const adv = "adv1"

	t.Run("余额充足扣费成功", func(t *testing.T) {
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

	t.Run("预算仅够最后一次时扣完即停", func(t *testing.T) {
		const adv = "adv2" // 预算 100，此前已扣 95 → 余 5
		if !c.TryDeduct(adv, 5) {
			t.Fatal("余额 5 扣 5 应成功")
		}
		if c.TryDeduct(adv, 5) {
			t.Fatal("余额 0，再次扣费应拒绝（req 只读闸此后将停投）")
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

	t.Run("多次扣费按序累计", func(t *testing.T) {
		const adv = "adv3" // 预算 50
		if !c.TryDeduct(adv, 30) || !c.TryDeduct(adv, 20) {
			t.Fatal("两次扣费（30+20）应都成功")
		}
		if c.TryDeduct(adv, 1) {
			t.Fatal("累计 50 = 预算，第三次应拒绝")
		}
		if spent, _ := c.Stats(adv); spent != 50 {
			t.Fatalf("spent 应为 50，实际 %v", spent)
		}
	})

	t.Run("跨日重置_Jakarta零点", func(t *testing.T) {
		const adv = "adv5" // 预算 20
		if !c.TryDeduct(adv, 15) {
			t.Fatal("扣费应成功")
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

	if sync != nil {
		baseline := map[string][2]float64{
			"adv1": {100, 0}, "adv2": {100, 95}, "adv3": {50, 0},
			"adv4": {30, 0}, "adv5": {20, 0}, "adv6": {10, 10},
		}

		t.Run("Sync_新增广告主纳入预算", func(t *testing.T) {
			const adv = "sync_new"
			if c.TryDeduct(adv, 1) {
				t.Fatal("同步前应未知")
			}
			all := copyMap(baseline)
			all[adv] = [2]float64{50, 0}
			sync(all)
			if !c.TryDeduct(adv, 10) {
				t.Fatal("同步后应可扣费（B2：新建广告主无需重启即可投放）")
			}
		})

		t.Run("Sync_已存在只刷新预算不动已耗", func(t *testing.T) {
			const adv = "sync_exist"
			all := copyMap(baseline)
			all[adv] = [2]float64{100, 0}
			sync(all)
			if !c.TryDeduct(adv, 30) {
				t.Fatal("扣费应成功")
			}
			// 模拟运营把日预算从 100 调到 200：DB 的 spent_today 仍是启动快照 0，
			// 同步后 spent 必须保持内存真相 30（不能拿 DB 旧值覆盖回去）。
			all2 := copyMap(baseline)
			all2[adv] = [2]float64{200, 0}
			sync(all2)
			spent, budget := c.Stats(adv)
			if spent != 30 {
				t.Fatalf("spent 应保持 30（内存真相，避免超发），实际 %v", spent)
			}
			if budget != 200 {
				t.Fatalf("budget 应刷新为 200（B3：改预算无需重启），实际 %v", budget)
			}
		})

		t.Run("Sync_移除已删除广告主", func(t *testing.T) {
			const adv = "sync_gone"
			all := copyMap(baseline)
			all[adv] = [2]float64{100, 0}
			sync(all)
			if !c.TryDeduct(adv, 10) {
				t.Fatal("应可扣费")
			}
			all2 := copyMap(baseline) // 不含 sync_gone → 模拟软删
			sync(all2)
			if c.TryDeduct(adv, 10) {
				t.Fatal("移除后（软删）应拒绝")
			}
		})
	}
}

func copyMap(m map[string][2]float64) map[string][2]float64 {
	out := make(map[string][2]float64, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
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
	runCtrlContract(t, ctrl, func(t time.Time) { now = t }, func(all map[string][2]float64) {
		ctrl.SyncBalances(all)
	})
}
