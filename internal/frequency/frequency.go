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

// SlotPolicy 广告位级频控策略（ConfigCache 的 ad_slots 配置）。
type SlotPolicy struct {
	Interval   time.Duration // 最小展示间隔（0 = 不限）
	DailyLimit int           // 滑动 24h 日频控上限（0 = 不限）
}

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
//
// 批量语义（PRD 5.5）：一次请求先 CheckSlot(count) 判断广告位总额度，
// 逐条物化时对每个广告主 CheckAndIncr；间隔检查只对比"请求前"的最后一次
// 下发时间——同一预取批次内的条目不互相卡间隔。
type Store interface {
	// CheckSlot 检查广告位级额度能否再容纳 count 条（只读不记账）。
	CheckSlot(appID, deviceID, slotID string, policy SlotPolicy, count int, now time.Time) bool

	// RecordSlot 记录一次下发（n 条），驱动间隔与日频控窗口。
	RecordSlot(appID, deviceID, slotID string, n int, now time.Time)

	// CheckAndIncr 原子检查并计数（广告主级多窗口 + 疲劳窗口）：
	// 通过则记账并返回 true；任一不过返回 false 且不记账。
	CheckAndIncr(appID, deviceID, slotID, advertiserID string, policy AdvPolicy, now time.Time) bool
}
