package budget

import (
	"math"
	"sync"
	"time"
)

// jakarta 预算自然日按印尼本地时区（WIB, UTC+7）重置——PRD"耗尽次日 0 点恢复"
// 按市场本地时间理解。
var jakarta = time.FixedZone("WIB", 7*60*60)

// balance 单广告主日预算状态。
type balance struct {
	budget float64
	spent  float64
}

// LedgerFn 预算流水异步写回调（main 装配时接 store 批量写）。
type LedgerFn func(advertiserID, opType string, amount float64)

// Memory 进程内实现（路线 A）。
//
// 真相分层：DB 是每日起点（启动时 LoadBudgetBalances 灌入），进程内维护
// 当日扣减，ledger 流水异步对账；跨日自动清零并记 daily_reset。
type Memory struct {
	mu       sync.Mutex
	balances map[string]*balance
	// wallets 广告主总余额（充值 - 扣费）。仅登记**已启用钱包**的广告主；
	// 未登记 = 不受总余额限制（存量广告主向后兼容）。
	wallets map[string]float64
	day     string // 当前预算日（Jakarta YYYY-MM-DD），跨日触发重置
	initDay bool   // 尚未校准预算日：首次操作按当前时钟设定，避免误清零
	ledger  LedgerFn
	now     func() time.Time
}

// NewMemory 创建预算控制器，balances 为启动加载的当日状态。
// 钱包余额随后由 SyncWallets 灌入（启动装配时调用）。
func NewMemory(balances map[string][2]float64, ledger LedgerFn) *Memory {
	m := &Memory{
		balances: make(map[string]*balance, len(balances)),
		wallets:  map[string]float64{},
		ledger:   ledger,
		now:      time.Now,
	}
	for id, b := range balances {
		m.balances[id] = &balance{budget: b[0], spent: b[1]}
	}
	m.initDay = true // 预算日首次操作时按当前时钟校准，不依赖 NewMemory 调用瞬间
	return m
}

// SetNow 注入时钟（测试用）。
func (m *Memory) SetNow(f func() time.Time) { m.now = f }

func (m *Memory) today() string { return m.now().In(jakarta).Format("2006-01-02") }

// rolloverLocked 跨日重置：预算日翻页时全部 spent 清零并记流水。
func (m *Memory) rolloverLocked() {
	if m.initDay {
		// 首次操作：仅记录当前预算日，不触发重置。否则若 NewMemory 调用时刻
		// 与注入时钟/首笔操作日期不一致（测试或动态调钟），会误判跨日而清空
		// 当日已耗。
		m.day = m.today()
		m.initDay = false
		return
	}
	if today := m.today(); today != m.day {
		for id, b := range m.balances {
			if b.spent > 0 && m.ledger != nil {
				m.ledger(id, "daily_reset", b.spent)
			}
			b.spent = 0
		}
		m.day = today
	}
}

func (m *Memory) TryDeduct(advertiserID string, amount float64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rolloverLocked()

	b, ok := m.balances[advertiserID]
	if !ok {
		return false // 未注册广告主（已删除/未加载）一律拒绝
	}
	if amount < 0 {
		return false
	}
	if b.spent+amount > b.budget {
		return false
	}
	b.spent += amount
	if m.ledger != nil {
		m.ledger(advertiserID, "deduct", amount)
	}
	return true
}

func (m *Memory) Stats(advertiserID string) (spent, budget float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rolloverLocked()
	b, ok := m.balances[advertiserID]
	if !ok {
		return 0, 0
	}
	return b.spent, b.budget
}

// StatsAll 批量版 Stats（BatchStats）：一次加锁遍历，跨日重置只做一次
//（逐个 Stats 每个都会触发 rolloverLocked，幂等但重复）。未注册的 ID 不入 map，
// 调用方按 (0,0) 处理，与 Stats 语义一致。
func (m *Memory) StatsAll(ids []string) map[string][2]float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rolloverLocked()
	out := make(map[string][2]float64, len(ids))
	for _, id := range ids {
		if b, ok := m.balances[id]; ok {
			out[id] = [2]float64{b.spent, b.budget}
		}
	}
	return out
}

// SyncBalances 用 DB 全量快照对齐内存预算表（配置热更新路径调用）。
//
// 语义严格遵循 budget.Syncer 接口契约，核心是**只刷新 daily_budget、不动 spent**：
// spent 的真相在进程内，DB 的 spent_today 是启动时的旧快照（不回写），
// 拿它覆盖会直接抹掉当日已消耗，导致预算超发。
func (m *Memory) SyncBalances(balances map[string][2]float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rolloverLocked()

	for id, b := range balances {
		if cur, ok := m.balances[id]; ok {
			cur.budget = b[0] // 已存在：只更新预算上限
			continue
		}
		m.balances[id] = &balance{budget: b[0], spent: b[1]} // 新增：按 DB 快照初始化
	}
	// DB 中已不存在（软删）：清掉内存状态，避免无主条目常驻
	for id := range m.balances {
		if _, ok := balances[id]; !ok {
			delete(m.balances, id)
		}
	}
}

func (m *Memory) HourlyCalibrate() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rolloverLocked()
	if m.ledger != nil {
		m.ledger("", "calibrate", 0)
	}
	return nil
}

// ---- 广告主总钱包 ----

// WalletBalance 总余额；未注册（未启用钱包）返回 MaxFloat64 = 不限制。
func (m *Memory) WalletBalance(advertiserID string) float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.wallets[advertiserID]
	if !ok {
		return math.MaxFloat64
	}
	return b
}

// WalletDeduct 总余额扣减；未注册放行（true，仍走 campaign 日预算闸）。
func (m *Memory) WalletDeduct(advertiserID string, amount float64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.wallets[advertiserID]
	if !ok {
		return true
	}
	if amount < 0 || b < amount {
		return false
	}
	m.wallets[advertiserID] = b - amount
	return true
}

// WalletCredit 充值入账；未注册的广告主在此登记（充值即启用钱包）。
func (m *Memory) WalletCredit(advertiserID string, amount float64) {
	if amount <= 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.wallets[advertiserID] += amount
}

// SyncWallets 用 DB 全量快照覆盖钱包余额（充值 / 对账后调用）。
func (m *Memory) SyncWallets(balances map[string]float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w := make(map[string]float64, len(balances))
	for id, b := range balances {
		w[id] = b
	}
	m.wallets = w
}

// 编译期接口实现检查。
var (
	_ Ctrl       = (*Memory)(nil)
	_ Syncer     = (*Memory)(nil)
	_ BatchStats = (*Memory)(nil)
)
