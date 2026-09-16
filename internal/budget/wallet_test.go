package budget

import (
	"math"
	"testing"
)

// 广告主总钱包语义：未启用 = 不限制（fail-open）；启用后按余额扣减、充值即时入账。
func TestMemoryWallet(t *testing.T) {
	m := NewMemory(nil, nil)

	// 未启用钱包（未收录）→ 不限制 / 放行，保证存量广告主向后兼容。
	if got := m.WalletBalance("adv1"); got != math.MaxFloat64 {
		t.Fatalf("未启用钱包余额 = %v，期望 MaxFloat64", got)
	}
	if !m.WalletDeduct("adv1", 100) {
		t.Fatal("未启用钱包应放行扣费")
	}

	// 启用钱包（SyncWallets 灌入余额）后按余额扣减。
	m.SyncWallets(map[string]float64{"adv1": 50})
	if got := m.WalletBalance("adv1"); got != 50 {
		t.Fatalf("余额 = %v，期望 50", got)
	}
	if !m.WalletDeduct("adv1", 30) {
		t.Fatal("余额 50 扣 30 应成功")
	}
	if got := m.WalletBalance("adv1"); got != 20 {
		t.Fatalf("扣减后余额 = %v，期望 20", got)
	}
	if m.WalletDeduct("adv1", 25) {
		t.Fatal("余额 20 扣 25 应失败")
	}
	if got := m.WalletBalance("adv1"); got != 20 {
		t.Fatalf("扣费失败不应改变余额，实际 %v", got)
	}
	// 恰好扣完 → 余额 0（停投临界）
	if !m.WalletDeduct("adv1", 20) || m.WalletBalance("adv1") != 0 {
		t.Fatal("应能恰好扣至 0")
	}
	if m.WalletBalance("adv1") > 0 {
		t.Fatal("余额为 0 时不应视为不限制")
	}

	// 充值即时入账；新广告主充值即启用。
	m.WalletCredit("adv1", 100)
	if got := m.WalletBalance("adv1"); got != 100 {
		t.Fatalf("充值后余额 = %v，期望 100", got)
	}
	m.WalletCredit("adv2", 5)
	if got := m.WalletBalance("adv2"); got != 5 {
		t.Fatalf("充值可登记新钱包，余额 = %v，期望 5", got)
	}
	// 非正金额忽略
	m.WalletCredit("adv2", 0)
	m.WalletCredit("adv2", -3)
	if got := m.WalletBalance("adv2"); got != 5 {
		t.Fatalf("非正充值应忽略，余额 = %v，期望 5", got)
	}
}

// SyncWallets 全量覆盖：DB 中已停用钱包（不在入参）应回到"不受限"。
func TestMemorySyncWalletsRemovesDisabled(t *testing.T) {
	m := NewMemory(nil, nil)
	m.SyncWallets(map[string]float64{"a": 10, "b": 20})
	m.SyncWallets(map[string]float64{"a": 10}) // b 停用
	if got := m.WalletBalance("b"); got != math.MaxFloat64 {
		t.Fatalf("停用钱包的广告主应回到不限制，实际 %v", got)
	}
	if got := m.WalletBalance("a"); got != 10 {
		t.Fatalf("a 余额 = %v，期望 10", got)
	}
}
