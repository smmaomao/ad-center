// Package frequency 提供频控的统一抽象。
//
// 路线 A（P0）：进程内时间戳队列（key 含 appID，多客户端隔离）。
// 路线 B（P1）：Redis ZSET + Lua 实现，接口不变，支撑双实例。
//
// 契约测试（contract_test.go）针对接口编写，内存/Redis 两个实现跑同一套
// 用例——这是 ARCHITECTURE.md §5.3.1 迁移手册的前置保险。
//
// 设计分工：**store 只存时间戳状态，策略参数由调用方传入**（来自
// ConfigCache 的广告位/广告主配置）——保持实现无配置依赖，Redis 版把
// 参数直接传给 Lua 脚本即可。
package frequency

import (
	"time"
)

// AdvPolicy 广告主级频控策略。
type AdvPolicy struct {
	Windows  []Window // 多窗口滑动频控（多档同时生效，如 3h/3 + 24h/10）
	FatigueN int      // 疲劳窗口：连续 N 次下发不重复同一广告主（0 = 不限）
}

// Window 滑动频控窗口（与 config.FreqWindow 同构，避免依赖环）。
type Window struct {
	WindowMinutes int
	MaxCount      int
}

// Store 是频控检查的唯一入口，实现必须并发安全。
//
// 用户身份 = (appID, deviceID)：appID 由服务端从 API Key 推导（客户端不可
// 自行声明），不同 App 的同名 deviceID 天然互不影响。
type Store interface {
	// CheckAndIncr 原子检查并计数（广告主级多窗口 + 疲劳窗口）：
	// 通过则记账并返回 true；任一不过返回 false 且不记账。
	CheckAndIncr(appID, deviceID, slotID, advertiserID string, policy AdvPolicy, now time.Time) bool

	// Check 只读检查多窗口 + 疲劳窗口（不下发、不计数）；通过返回 true。
	// 用于决策期——"能否下发"判断，真正的计数放到 Record（真实曝光时）。
	Check(appID, deviceID, slotID, advertiserID string, policy AdvPolicy, now time.Time) bool

	// Record 仅记账（多窗口 + 疲劳序列）；在真实观看/曝光时调用。
	Record(appID, deviceID, slotID, advertiserID string, policy AdvPolicy, now time.Time)
}

// CheckItem 批量只读检查的一条入参（一次决策中的单个候选）。
type CheckItem struct {
	SlotID       string    // 频控维度：素材（等价旧广告位）
	AdvertiserID string    // 频控维度：广告任务 campaign
	Policy       AdvPolicy // 由调用方从 campaign 配置推导
}

// BatchChecker 批量只读频控检查（**可选能力**，与 budget.BatchStats 同款模式）。
//
// 背景：决策热路径里原来是「每候选一次 Check」= 每候选一条 EVALSHA。一次
// /v1/ad/list 跨 5 个样式、每样式 N 个候选，命令数 = 候选×样式数（实测可达
// 20+ 条/请求），是 Redis 用量的最大单项。批量版把「同一 app+device 下 N 个
// 候选」压成一次脚本往返——这些键都带 {fr} hash tag，Cluster 下同一 slot，
// 不会有 CROSSSLOT。
//
// 调用方约定：返回 nil / 长度不符 = 未提供或失败，必须逐个回退 Check，
// 保持既有 fail-open 语义不变。
type BatchChecker interface {
	// CheckBatch 返回与 items 等长的结果切片（true = 放行）；失败返回 nil。
	CheckBatch(appID, deviceID string, items []CheckItem, now time.Time) []bool
}
