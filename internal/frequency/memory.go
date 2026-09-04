package frequency

import (
	"context"
	"sync"
	"time"
)

// slotState 广告位级状态（per app+device+slot）。
type slotState struct {
	lastFill time.Time   // 间隔检查锚点（请求前最后一次下发）
	fills    []time.Time // 24h 滑动窗口内的下发时间（count 条记 count 个时间戳）
	recent   []string    // 最近下发的广告主序列（疲劳窗口，长度 ≤ maxFatigue）
}

// advState 广告主级状态（per app+device+advertiser，跨广告位共享——
// 同一广告主对同一用户的频控是全局的，换广告位不该重复轰炸）。
type advState struct {
	fills []time.Time // 各窗口共用的时间戳序列（追加序天然升序，滑动裁剪）
}

// MemoryStore 进程内实现（路线 A）。
//
// 数据结构 = 时间戳队列：检查是数组遍历（窗口内计数），微秒级、零 IO；
// 过期条目由 Prune 周期清理，maxWindow 之外的状态整体出局。
type MemoryStore struct {
	mu        sync.Mutex
	slots     map[string]*slotState
	advs      map[string]*advState
	maxWindow time.Duration // 全部窗口的最大长度（TTL，建议 ≥ 24h）
}

// NewMemory 创建内存频控存储。
func NewMemory(maxWindow time.Duration) *MemoryStore {
	return &MemoryStore{
		slots:     map[string]*slotState{},
		advs:      map[string]*advState{},
		maxWindow: maxWindow,
	}
}

func (m *MemoryStore) CheckSlot(appID, deviceID, slotID string, policy SlotPolicy, count int, now time.Time) bool {
	if count <= 0 {
		return true
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	st := m.slots[appID+":"+deviceID+":"+slotID]
	if st == nil {
		return true // 该设备此广告位从未下发
	}
	// 最小展示间隔：对比请求前最后一次下发
	if policy.Interval > 0 && !st.lastFill.IsZero() && now.Sub(st.lastFill) < policy.Interval {
		return false
	}
	// 滑动 24h 日频控
	if policy.DailyLimit > 0 {
		n := countSince(st.fills, now.Add(-24*time.Hour))
		if n+count > policy.DailyLimit {
			return false
		}
	}
	return true
}

func (m *MemoryStore) RecordSlot(appID, deviceID, slotID string, n int, now time.Time) {
	if n <= 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	key := appID + ":" + deviceID + ":" + slotID
	st := m.slots[key]
	if st == nil {
		st = &slotState{}
		m.slots[key] = st
	}
	st.lastFill = now
	for i := 0; i < n; i++ {
		st.fills = append(st.fills, now)
	}
	st.fills = pruneBefore(st.fills, now.Add(-m.maxWindow))
}

func (m *MemoryStore) CheckAndIncr(appID, deviceID, slotID, advertiserID string, policy AdvPolicy, now time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	slotKey := appID + ":" + deviceID + ":" + slotID
	st := m.slots[slotKey]

	// 疲劳窗口：该广告位最近 N 次下发的广告主里不含当前者
	if policy.FatigueN > 0 && st != nil {
		if recentContains(st.recent, advertiserID, policy.FatigueN) {
			return false
		}
	}

	// 广告主级多窗口滑动频控（全局，跨广告位）
	if len(policy.Windows) > 0 {
		adv := m.advs[appID+":"+deviceID+":"+advertiserID]
		if adv != nil {
			for _, w := range policy.Windows {
				if w.MaxCount > 0 && w.WindowMinutes > 0 &&
					countSince(adv.fills, now.Add(-time.Duration(w.WindowMinutes)*time.Minute)) >= w.MaxCount {
					return false
				}
			}
		}
	}

	// 全部通过：记账（失败路径到此为止，状态未变）
	if st == nil {
		st = &slotState{}
		m.slots[slotKey] = st
	}
	st.recent = pushRecent(st.recent, advertiserID, policy.FatigueN)

	advKey := appID + ":" + deviceID + ":" + advertiserID
	adv := m.advs[advKey]
	if adv == nil {
		adv = &advState{}
		m.advs[advKey] = adv
	}
	adv.fills = append(adv.fills, now)
	adv.fills = pruneBefore(adv.fills, now.Add(-m.maxWindow))
	return true
}

// Prune 清理完全过期的状态（后台周期调用，冷设备自动出局）。
// recent 序列无时间戳，跟随 slotState 整体清理：lastFill 早于 maxWindow 即整条删除。
func (m *MemoryStore) Prune(now time.Time) {
	cutoff := now.Add(-m.maxWindow)
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, st := range m.slots {
		if st.lastFill.Before(cutoff) {
			delete(m.slots, k)
		} else {
			st.fills = pruneBefore(st.fills, cutoff)
		}
	}
	for k, adv := range m.advs {
		adv.fills = pruneBefore(adv.fills, cutoff)
		if len(adv.fills) == 0 {
			delete(m.advs, k)
		}
	}
}

// Len 报告当前状态条数（监控/测试用）。
func (m *MemoryStore) Len() (slots, advs int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.slots), len(m.advs)
}

func recentContains(recent []string, advertiserID string, n int) bool {
	start := len(recent) - n
	if start < 0 {
		start = 0
	}
	for _, id := range recent[start:] {
		if id == advertiserID {
			return true
		}
	}
	return false
}

func pushRecent(recent []string, advertiserID string, n int) []string {
	// 疲劳窗口上限 10，防异常配置撑爆内存
	if n < 1 {
		n = 1
	}
	if n > 10 {
		n = 10
	}
	recent = append(recent, advertiserID)
	if len(recent) > n {
		recent = recent[len(recent)-n:]
	}
	return recent
}

func countSince(times []time.Time, since time.Time) int {
	n := 0
	for _, t := range times {
		if !t.Before(since) {
			n++
		}
	}
	return n
}

func pruneBefore(times []time.Time, before time.Time) []time.Time {
	// 追加序天然升序：找第一个 >= before 的位置整体前移
	cut := 0
	for cut < len(times) && times[cut].Before(before) {
		cut++
	}
	switch cut {
	case 0:
		return times
	case len(times):
		return nil
	default:
		return times[cut:]
	}
}

// 编译期接口实现检查。
var _ Store = (*MemoryStore)(nil)

var _ = context.Background // 保留 context 导入位（接口签名未来扩展用）
