// Package frequency 提供频控的统一抽象（日频控 / 展示间隔 / 疲劳窗口）。
//
// 路线 A（P0）：进程内 LRU（key: userID+slotID，TTL 24h）。
// 路线 B（P1）：Redis 实现，接口不变，支撑双实例。
package frequency

import (
	"context"
	"time"
)

// Store 是频控检查的唯一入口，实现必须并发安全。
type Store interface {
	// CheckAndIncr 原子检查并计数：允许展示则记录并返回 true，否则 false。
	//
	// 检查项（PRD FR-04 高级策略）：
	//   - 日频控：单用户每日该广告位最大展示次数
	//   - 展示间隔：两次展示间最短间隔
	//   - 疲劳窗口：连续 N 次内不重复展示同一广告主
	//
	// 阶段 1.5 实现时会携带策略参数（上限/间隔/窗口），此处签名先固化调用面。
	CheckAndIncr(ctx context.Context, userID, slotID, advertiserID string, now time.Time) bool
}
