// Package fatigue 用户疲劳度（全局频控）控制：按「用户 × 素材（creative）」累加
// 观看次数，两个维度分别限流——控制1（短窗口）+ 控制2（滚动 24h）。
//
// 维度用户标识采用决策请求里的 DeviceID（与现有频控主体一致）。
//
// Redis key 设计（与需求一致，prefix 由调用方统一加项目前缀如 "adcenter:"）：
//
//	控制1: user:ad:limit:{userID}:{creativeID}   过期 = WindowMinutes 分钟（非滑动窗口）
//	控制2: user:ad:daily:{userID}:{creativeID}   过期 = 24h（非自然日、非滑动）
//
// 语义选择（需求明确"不需要精确滑动窗口"）：
//   - 控制1 的窗口从【第一次观看】起算整段 WindowMinutes，过期后计数清零，不做滚动。
//   - 控制2 同理，从第一次观看起算整段 24h。
//   - 因此只在计数从 0→1 的那次写入设置 EXPIRE，后续 INCR 不动 TTL。
package fatigue

import (
	"adcenter/internal/config"
)

// Store 用户疲劳度存储。
//
// 计数时机：Check 仅读取（决策时判断该素材是否应隐藏），Record 在用户实际观看
// （impression 事件）后调用。两者解耦——决策缓存命中不会误增计数，疲劳上限始终
// 以真实观看次数为准。
type Store interface {
	// Check 仅读取：该用户对该素材是否已达任一疲劳上限。不计数。
	// 返回 (blocked, reason)，reason ∈ {"window_exceeded","daily_exceeded"}。
	Check(userID, creativeID string, cfg config.FatigueConfig) (blocked bool, reason string)
	// Record 在用户实际观看（impression）后调用：两个维度各 +1。
	Record(userID, creativeID string, cfg config.FatigueConfig)
}
