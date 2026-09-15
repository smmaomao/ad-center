// Package budget 提供预算控制的统一抽象。
//
// 路线 A（P0）：进程内实现，异步落库 budget_ledger 对账。
// 路线 B（P1）：Redis Lua 实现，接口不变，支撑双实例。
//
// 契约测试（contract_test.go）针对接口编写，内存/Redis 实现跑同一套用例
// （ARCHITECTURE.md §5.3.1 迁移前置保险）。
package budget

// Ctrl 是预算控制的唯一入口，实现必须并发安全。
//
// 计费模型（migration 000008）：req 阶段不扣钱（引擎只做只读闸：
// spent >= budget 即停投），扣费全部发生在真实计费事件到达时——
//   - billing_mode=cpm → impression 回执（单次 BiddingPrice/1000）
//   - billing_mode=cpc → click 回执（单次 BiddingPrice）
//   - billing_mode=cpa → 归因方 S2S 转化回调（金额 = cpa_event_prices[event]）
//
// 曾经的"req 预扣 → 回执 commit/rollback"模型已废弃：它把单次安装价
// 当作每次下发的价格，预算会虚假耗尽（见 ARCHITECTURE 变更记录）。
type Ctrl interface {
	// TryDeduct 原子确认扣费：余额充足则扣减并返回 true，否则 false（余额不变）。
	// 仅在计费事件（impression/click/S2S 转化）处理路径上调用，决策路径不调用。
	TryDeduct(advertiserID string, amount float64) bool

	// Stats 返回 (今日已耗, 日预算)——引擎打分的消耗进度因子与只读预算闸用。
	// 未知广告主返回 (0, 0)。
	Stats(advertiserID string) (spent, budget float64)

	// HourlyCalibrate 每小时预算平滑校准记账（PRD 5.2；
	// 权重调整本身在引擎打分内实时进行，此处落校准流水供审计）。
	HourlyCalibrate() error
}

// Syncer 预算控制器的可选能力：由配置变更驱动的日预算同步。
//
// 为什么需要（B2/B3）：广告主列表与日预算由 ConfigCache 热更新（秒级生效），
// 但预算状态是进程内独立维护的（启动时从 DB 灌入后不再回查）。不同步会导致：
//   - B2：后台新建的广告主，配置已进快照可以参排，但预算表里没有它 →
//     TryDeduct 对未知 ID 恒返回 false → 该广告主在进程重启前一条都投不出去
//   - B3：后台调高/调低日预算，进程内仍是启动时的旧值 → 改了不生效
//
// 契约（P1 Redis 实现必须同语义）：
//   - 入参是 DB 的**全量** (daily_budget, spent_today) 快照
//   - 新增的广告主：以入参的 (budget, spent) 初始化
//   - 已存在的广告主：**只刷新 daily_budget，绝不覆盖进程内已累计的 spent**
//     ——spent 的真相在内存（DB 的 spent_today 只是启动时的快照，目前不回写）
//   - 入参中不存在的广告主（已软删）：移除其内存状态
//   - 实现必须并发安全
type Syncer interface {
	SyncBalances(balances map[string][2]float64)
}
