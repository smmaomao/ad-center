package fatigue

import (
	"sync"
	"time"

	"adcenter/internal/config"
)

// MemoryStore 进程内实现（单实例 / 本地开发 / 测试）。
// 结构与 Redis 一致：两条计数 + 各自过期时间，不做滑动窗口。
type MemoryStore struct {
	mu  sync.Mutex
	lim map[string]entry
	dai map[string]entry
	now func() time.Time
}

type entry struct {
	n     int
	expAt time.Time
}

// NewMemory 构造内存版疲劳度存储。
func NewMemory() *MemoryStore {
	return &MemoryStore{
		lim: map[string]entry{},
		dai: map[string]entry{},
		now: time.Now,
	}
}

// Check 仅读取：任一维度在有效期内达到上限即阻止。
func (m *MemoryStore) Check(userID, creativeID string, cfg config.FatigueConfig) (bool, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	if e, ok := m.lim[key(userID, creativeID)]; ok && now.Before(e.expAt) && e.n >= cfg.WindowMax {
		return true, "window_exceeded"
	}
	if e, ok := m.dai[key(userID, creativeID)]; ok && now.Before(e.expAt) && e.n >= cfg.DailyMax {
		return true, "daily_exceeded"
	}
	return false, ""
}

// Record 两条计数各 +1；过期则重置（非滑动窗口语义）。
func (m *MemoryStore) Record(userID, creativeID string, cfg config.FatigueConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	k := key(userID, creativeID)
	if e, ok := m.lim[k]; !ok || !now.Before(e.expAt) {
		m.lim[k] = entry{n: 1, expAt: now.Add(time.Duration(cfg.WindowMinutes) * time.Minute)}
	} else {
		m.lim[k] = entry{n: e.n + 1, expAt: e.expAt}
	}
	if e, ok := m.dai[k]; !ok || !now.Before(e.expAt) {
		m.dai[k] = entry{n: 1, expAt: now.Add(24 * time.Hour)}
	} else {
		m.dai[k] = entry{n: e.n + 1, expAt: e.expAt}
	}
}

func key(userID, creativeID string) string { return userID + "\x00" + creativeID }

var _ Store = (*MemoryStore)(nil)
