package budget

import (
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
	day      string // 当前预算日（Jakarta YYYY-MM-DD），跨日触发重置
	ledger   LedgerFn
	now      func() time.Time
}

// NewMemory 创建预算控制器，balances 为启动加载的当日状态。
func NewMemory(balances map[string][2]float64, ledger LedgerFn) *Memory {
	m := &Memory{
		balances: make(map[string]*balance, len(balances)),
		ledger:   ledger,
		now:      time.Now,
	}
	for id, b := range balances {
		m.balances[id] = &balance{budget: b[0], spent: b[1]}
	}
	m.day = m.today()
	return m
}

// SetNow 注入时钟（测试用）。
func (m *Memory) SetNow(f func() time.Time) { m.now = f }

func (m *Memory) today() string { return m.now().In(jakarta).Format("2006-01-02") }

// rolloverLocked 跨日重置：预算日翻页时全部 spent 清零并记流水。
func (m *Memory) rolloverLocked() {
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

func (m *Memory) Commit(advertiserID string, amount float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rolloverLocked()
	if m.ledger != nil {
		m.ledger(advertiserID, "commit", amount)
	}
	return nil
}

func (m *Memory) Rollback(advertiserID string, amount float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rolloverLocked()

	if b, ok := m.balances[advertiserID]; ok && amount > 0 {
		b.spent -= amount
		if b.spent < 0 {
			b.spent = 0
		}
	}
	if m.ledger != nil {
		m.ledger(advertiserID, "rollback", amount)
	}
	return nil
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

func (m *Memory) HourlyCalibrate() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rolloverLocked()
	if m.ledger != nil {
		m.ledger("", "calibrate", 0)
	}
	return nil
}

// 编译期接口实现检查。
var _ Ctrl = (*Memory)(nil)
