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
	// 用户身份 = (appID, deviceID)：多客户端 App 隔离由服务端从 API Key
	// 推导 appID（客户端不可自行声明），不同 App 的同名 ID 天然互不影响。
	//
	// 检查项（两级，任一不过即拒绝）：
	//   广告位级：滑动24h日频控 / 最小展示间隔 / 疲劳窗口（连续N次不重复同一广告主）
	//   广告主级：多窗口滑动频控 freq_windows（多档同时生效，如 3h/3 + 24h/10）
	CheckAndIncr(ctx context.Context, appID, deviceID, slotID, advertiserID string, now time.Time) bool
}
