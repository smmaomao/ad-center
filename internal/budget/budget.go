// Package budget 提供预算控制的统一抽象。
//
// 路线 A（P0）：进程内实现，定时落库 budget_ledger 对账。
// 路线 B（P1）：Redis Lua 实现，接口不变，支撑双实例。
package budget

import "context"

// Ctrl 是预算控制的唯一入口，实现必须并发安全。
type Ctrl interface {
	// TryDeduct 原子预扣：余额充足则扣减并返回 true，否则 false。
	// amount 为本次预估消耗（美元）。
	TryDeduct(ctx context.Context, advertiserID string, amount float64) bool

	// Commit 确认一次预扣：事件回执（曝光/点击）到达后结转。
	Commit(ctx context.Context, advertiserID string, amount float64) error

	// Rollback 回滚一次预扣：填充未兑现（超时未曝光）时返还额度。
	Rollback(ctx context.Context, advertiserID string, amount float64) error

	// HourlyCalibrate 每小时预算平滑校准（PRD 5.2）：
	// 实际进度 vs 预期进度偏差超 ±10% 时调整流量权重。
	HourlyCalibrate(ctx context.Context) error
}
