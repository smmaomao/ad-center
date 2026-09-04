// Package budget 提供预算控制的统一抽象。
//
// 路线 A（P0）：进程内实现，异步落库 budget_ledger 对账。
// 路线 B（P1）：Redis Lua 实现，接口不变，支撑双实例。
//
// 契约测试（contract_test.go）针对接口编写，内存/Redis 实现跑同一套用例
// （ARCHITECTURE.md §5.3.1 迁移前置保险）。
package budget

// Ctrl 是预算控制的唯一入口，实现必须并发安全。
type Ctrl interface {
	// TryDeduct 原子预扣：余额充足则扣减并返回 true，否则 false（余额不变）。
	// amount 为本次预估消耗（美元，决策时以出价估计）。
	TryDeduct(advertiserID string, amount float64) bool

	// Commit 确认一次预扣（事件回执到达后结转；余额在预扣时已扣，此处只记账）。
	Commit(advertiserID string, amount float64) error

	// Rollback 回滚一次预扣（填充未兑现时返还额度）。
	Rollback(advertiserID string, amount float64) error

	// Stats 返回 (今日已耗, 日预算)——引擎打分的消耗进度因子用。
	// 未知广告主返回 (0, 0)。
	Stats(advertiserID string) (spent, budget float64)

	// HourlyCalibrate 每小时预算平滑校准记账（PRD 5.2；
	// 权重调整本身在引擎打分内实时进行，此处落校准流水供审计）。
	HourlyCalibrate() error
}
