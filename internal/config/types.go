// Package config 提供配置缓存：启动全量加载广告主/广告位/素材到内存，
// 通过 Postgres LISTEN/NOTIFY 秒级热更新，60s 定时对账防通知丢失。
//
// 域模型（App/Advertiser/Slot/Creative）定义在此包——它们就是"被缓存的配置"；
// store 依赖本包加载，engine 依赖本包读取快照，无环。
package config

import (
	"encoding/json"
	"slices"
	"time"
)

// App 客户端 App 注册信息（多租户隔离锚点）。
type App struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	APIKeyPrefix string `json:"api_key_prefix"`
	APIKeyHash   string `json:"-"`      // sha256 hex，用于 X-Api-Key 匹配
	Status       string `json:"status"` // active / paused
}

// Active 报告该 App 是否可服务。
func (a *App) Active() bool { return a.Status == "active" }

// FreqWindow 广告主级滑动频控窗口（advertisers.freq_windows jsonb 数组元素）。
// 字段名与 migration 000003 默认值一致：{"window_minutes":180,"max":3}。
type FreqWindow struct {
	WindowMinutes int `json:"window_minutes"`
	MaxCount      int `json:"max"`
}

// Targeting 定向条件（advertisers.targeting jsonb）。空切片 = 不限。
type Targeting struct {
	Countries []string `json:"countries,omitempty"` // ISO-3166 alpha-2
	Languages []string `json:"languages,omitempty"`
}

// Match 判断请求上下文是否命中定向（未配置的字段视为全通过）。
func (t Targeting) Match(country, language string) bool {
	if len(t.Countries) > 0 && !contains(t.Countries, country) {
		return false
	}
	if len(t.Languages) > 0 && !contains(t.Languages, language) {
		return false
	}
	return true
}

func contains(list []string, s string) bool { return slices.Contains(list, s) }

// Advertiser 广告主（全局共享：预算/KPI/素材跨 App 一份）。
type Advertiser struct {
	ID                 string       `json:"id"`
	Name               string       `json:"name"`
	Tier               int          `json:"tier"`
	Status             string       `json:"status"` // active / paused / budget_exhausted
	TargetCPI          float64      `json:"target_cpi"`
	ActualCPI          float64      `json:"actual_cpi"`
	DailyBudget        float64      `json:"daily_budget"`
	ConsumeSpeed       string       `json:"consume_speed"` // even / accelerated / asap
	BiddingMode        string       `json:"bidding_mode"`  // cpi / cpa / revenue_share
	BiddingPrice       float64      `json:"bidding_price"`
	GuaranteedEnabled  bool         `json:"guaranteed_enabled"`
	GuaranteedMinShare float64      `json:"guaranteed_min_share"`
	Targeting          Targeting    `json:"targeting"`
	FreqWindows        []FreqWindow `json:"freq_windows"`
	EndAt              *time.Time   `json:"end_at,omitempty"`
}

// Active 报告广告主当前是否可参排（状态活跃且未过投放截止时间）。
func (a *Advertiser) Active(now time.Time) bool {
	if a.Status != "active" {
		return false
	}
	if a.EndAt != nil && !now.Before(*a.EndAt) {
		return false
	}
	return true
}

// Achievement KPI 达成率 = actualCpi / targetCpi（≤1 达标；目标为 0 时按 1 处理）。
func (a *Advertiser) Achievement() float64 {
	if a.TargetCPI <= 0 {
		return 1
	}
	return a.ActualCPI / a.TargetCPI
}

// Slot 广告位（归属 App，客户端以 slot_key 引用）。
type Slot struct {
	ID                  string         `json:"id"`
	AppID               string         `json:"app_id"`
	Key                 string         `json:"key"` // slot_key，客户端稳定标识
	Name                string         `json:"name"`
	Type                string         `json:"type"` // rewarded_video / splash / interstitial / feed
	Status              string         `json:"status"`
	FreqDailyLimit      int            `json:"freq_daily_limit"`      // 滑动 24h
	FreqIntervalMinutes int            `json:"freq_interval_minutes"` // 最小展示间隔
	FreqFatigueWindow   int            `json:"freq_fatigue_window"`   // 连续 N 次不重复
	Priorities          []FillPriority `json:"priorities"`            // 仅 enabled，position 升序
}

// Active 报告广告位是否可服务。
func (s *Slot) Active() bool { return s.Status == "active" }

// FillPriority 填充来源条目。
type FillPriority struct {
	ID              string  `json:"id"`
	SourceType      string  `json:"source_type"` // advertiser / max / fallback
	AdvertiserID    string  `json:"advertiser_id,omitempty"`
	ExpectedECPM    float64 `json:"expected_ecpm"`
	GuaranteedShare float64 `json:"guaranteed_share"` // [0,1]
	Weight          float64 `json:"weight"`
}

// Creative 素材。
type Creative struct {
	ID           string  `json:"id"`
	AdvertiserID string  `json:"advertiser_id"`
	Name         string  `json:"name"`
	MediaType    string  `json:"media_type"`   // video / image / html
	StoragePath  string  `json:"storage_path"` // R2 对象 key 或完整 URL（html）
	Orientation  string  `json:"orientation"`  // portrait / landscape / square / any
	Width        int     `json:"width,omitempty"`
	Height       int     `json:"height,omitempty"`
	DurationMS   int     `json:"duration_ms,omitempty"`
	Status       string  `json:"status"` // testing / active / paused
	Weight       float64 `json:"weight"`
	ABGroup      string  `json:"ab_group,omitempty"`
}

// Snapshot 不可变配置快照：整体构建、原子替换、只读使用。
type Snapshot struct {
	Apps                  map[string]*App        // app_id → App
	AppByKeyHash          map[string]*App        // api_key_hash → App
	Advertisers           map[string]*Advertiser // advertiser_id → Advertiser
	Slots                 map[string]*Slot       // slot_id → Slot
	SlotsByKey            map[string]*Slot       // slot_key → Slot
	CreativesByAdvertiser map[string][]*Creative // advertiser_id → 活跃素材（weight 降序）
}

// ParseTargeting 解析 targeting jsonb（空值返回零值 Targeting = 全通过）。
func ParseTargeting(b []byte) (Targeting, error) {
	var t Targeting
	if len(b) == 0 {
		return t, nil
	}
	if err := json.Unmarshal(b, &t); err != nil {
		return Targeting{}, err
	}
	return t, nil
}

// ParseFreqWindows 解析 freq_windows jsonb。
func ParseFreqWindows(b []byte) ([]FreqWindow, error) {
	if len(b) == 0 {
		return nil, nil
	}
	var w []FreqWindow
	if err := json.Unmarshal(b, &w); err != nil {
		return nil, err
	}
	return w, nil
}
