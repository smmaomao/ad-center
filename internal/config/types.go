// Package config 提供配置缓存：启动全量加载广告主/广告位/素材到内存，
// 通过 Postgres LISTEN/NOTIFY 秒级热更新，60s 定时对账防通知丢失。
//
// 域模型（App/Advertiser/Slot/Creative）定义在此包——它们就是"被缓存的配置"；
// store 依赖本包加载，engine 依赖本包读取快照，无环。
package config

import (
	"encoding/json"
	"math/rand/v2"
	"slices"
	"time"
)

// App 客户端 App 注册信息（多租户隔离锚点）。
type App struct {
	ID         string `json:"id"`
	Name        string `json:"name"`
	APIKeyHash  string `json:"-"`      // sha256 hex，用于 X-Api-Key 匹配
	Status      string `json:"status"` // active / paused
	CallbackURL string `json:"callback_url,omitempty"` // 业务后端 S2S 接收地址（激励视频完播回调用）
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

// Advertiser 广告主（全局共享：计费锚点 / 素材跨 App 一份）。
//
// 运营期 KPI（目标/实际 CPI）、投放结束日期、消耗节奏、KPI 考核口径、下发有效期
// 均已下沉到广告任务（campaign，见 Campaign）——campaign 是 KPI / 预算 / 排期的
// 执行粒度，广告主只承担「身份 + 计费锚点」（bidding_price / billing_mode /
// cpa_event_prices / freq_windows）。
type Advertiser struct {
	ID                 string                `json:"id"`
	Name               string                `json:"name"`
	Status             string                `json:"status"` // active / paused / budget_exhausted
	BiddingPrice       float64               `json:"bidding_price"` // 计费单价上限（cpm=每千次、cpc/cpa=每事件）
	BiddingPriceMin    float64               `json:"bidding_price_min,omitempty"` // 单价下限；0=未配置→退化为固定 BiddingPrice
	BillingMode        string                `json:"billing_mode"`  // cpm / cpc / cpa（扣费锚点，migration 000008）
	CPAEventPrices     map[string][2]float64 `json:"cpa_event_prices,omitempty"` // 事件→[min,max] 区间
	Targeting          Targeting             `json:"targeting"`
	FreqWindows        []FreqWindow          `json:"freq_windows"`
}

// Active 报告广告主账户当前是否可参排（状态活跃）。投放结束日期 / 排期由广告任务
// （campaign）控制，见 Campaign.Active。
func (a *Advertiser) Active(now time.Time) bool {
	return a.Status == "active"
}

// DefaultDeliverTTLMinutes 广告下发到客户端的默认展示有效期（分钟）。
// campaign 未配置 deliver_ttl_minutes（<=0）时采用此兜底值。
const DefaultDeliverTTLMinutes = 10

// KPI 达成率口径（PRD 6.1 / FR-02 / FR-08）：达成率 = targetCPI / actualCPI；
// 冷启动（actualCPI≤0）按中性 1.0；钳制到 [minAchievement, maxAchievement]。
// 该口径现由广告任务（campaign，KPI 执行粒度）持有，见 Campaign.Achievement()；
// 归集层（广告主/产品）的达成率由 store_campaigns.go 的 Rollup 用同一口径计算。
const (
	minAchievement = 0.25
	maxAchievement = 4.0
)

// clampAchievement 把达成率限制在 [minAchievement, maxAchievement]。
func clampAchievement(r float64) float64 {
	if r < minAchievement {
		return minAchievement
	}
	if r > maxAchievement {
		return maxAchievement
	}
	return r
}

// Slot 广告位（归属 App，客户端以 slot_key 引用）。
type Slot struct {
	ID                  string         `json:"id"`
	AppID               string         `json:"app_code"`
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

// Creative 素材（纯资产：尺寸 / 时长 / 样式 / 定向；商业与策略在 campaign 维度）。
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
	ABGroup      string  `json:"ab_group,omitempty"`

	// 展现样式（多选）：splash 开屏 / rewarded_video 激励视频 / interstitial 插屏
	// / feed 信息流 / banner。素材直接声明支持哪些样式，不再依赖 slot。
	Styles []string `json:"styles,omitempty"`
	// 投放目标 App（多选）；空 = 投放到全部 App
	TargetApps []string `json:"target_apps,omitempty"`
}

// PricingBenchmark 平台计费标准线（系统设置里配，全局一份）。
//
// 用于把不同扣费维度的出价拉平到同一个"100 分制"天平上比较：
//
//	出价基准分(Price_Score) = 素材实际出价 ÷ 该扣费模式的标准线 × 100
//
// 例：CPC 标准线 5，素材出价 6 → 120 分；CPA首充标准线 20，素材出价 10 → 50 分。
// 这样"CPC 6 元"和"CPA 10 元"就能直接比大小了。
type PricingBenchmark struct {
	CPM              float64 `json:"cpm"`
	CPC              float64 `json:"cpc"`
	CPAInstall       float64 `json:"cpa_install"`
	CPAActivate      float64 `json:"cpa_activate"`
	CPARegister      float64 `json:"cpa_register"`
	CPAFirstPurchase float64 `json:"cpa_first_purchase"`
}

// DefaultPricingBenchmark 标准线未配置时的兜底值（避免除零导致基准分爆炸）。
func DefaultPricingBenchmark() *PricingBenchmark {
	return &PricingBenchmark{
		CPM: 15, CPC: 5,
		CPAInstall: 10, CPAActivate: 12, CPARegister: 15, CPAFirstPurchase: 20,
	}
}

// BenchmarkFor 按扣费方式（+ CPA 具体事件）取对应的标准线；未配置或为 0 时
// 回退到兜底值，保证基准分始终可计算。
func (b *PricingBenchmark) BenchmarkFor(billingMode, cpaEvent string) float64 {
	if b == nil {
		b = DefaultPricingBenchmark()
	}
	var v float64
	switch billingMode {
	case "cpm":
		v = b.CPM
	case "cpc":
		v = b.CPC
	case "cpa":
		switch cpaEvent {
		case "install":
			v = b.CPAInstall
		case "activate":
			v = b.CPAActivate
		case "register":
			v = b.CPARegister
		case "first_purchase":
			v = b.CPAFirstPurchase
		}
	}
	if v <= 0 {
		// 该项没配 → 用兜底，避免除零
		d := DefaultPricingBenchmark()
		switch billingMode {
		case "cpm":
			return d.CPM
		case "cpc":
			return d.CPC
		case "cpa":
			switch cpaEvent {
			case "install":
				return d.CPAInstall
			case "activate":
				return d.CPAActivate
			case "register":
				return d.CPARegister
			case "first_purchase":
				return d.CPAFirstPurchase
			default:
				return d.CPAInstall
			}
		default:
			return d.CPC
		}
	}
	return v
}

// Campaign 广告任务（投放执行粒度）。预算闸与扣费按 campaign 各自控制，
// 因此 snapshot 同时持有 Campaigns 与 creative→campaign 归属映射。
type Campaign struct {
	ID                string     `json:"id"`
	AdvertiserID      string     `json:"advertiser_id"`
	Name              string     `json:"name"`
	Status            string     `json:"status"` // active / paused
	DailyBudget       float64    `json:"daily_budget"`
	SpentToday        float64    `json:"spent_today"`
	ConsumeSpeed      string     `json:"consume_speed"`
	TargetCPI         float64    `json:"target_cpi"`
	ActualCPI         float64    `json:"actual_cpi"`
	BillingMode       string     `json:"billing_mode"`   // cpm / cpc / cpa（计费方式，引擎排序取数）
	BiddingPrice      float64    `json:"bidding_price"`  // 出价（引擎出价基准分取数）
	PriorityScore     float64    `json:"priority_score"` // 优先级系数（替代原素材 weight）
	BiddingMode       string     `json:"bidding_mode"` // cpi / cpa / revenue_share（KPI 考核口径）
	DeliverTTLMinutes int        `json:"deliver_ttl_minutes"`
	CreativeIDs       []string   `json:"creative_ids"`
	StartAt           *time.Time `json:"start_at,omitempty"`
	EndAt             *time.Time `json:"end_at,omitempty"`
	LandingURL        string     `json:"landing_url,omitempty"` // 落地页 URL（click_url 来源）
}

// Active 报告广告任务当前是否可参排：状态活跃且在投放排期内。
func (c *Campaign) Active(now time.Time) bool {
	if c.Status != "active" {
		return false
	}
	if c.StartAt != nil && now.Before(*c.StartAt) {
		return false
	}
	if c.EndAt != nil && now.After(*c.EndAt) {
		return false
	}
	return true
}

// Achievement 计算 KPI 达成率（与旧 Advertiser 口径一致：target/actual、
// actual≤0 取中性 1、钳制 [minAchievement, maxAchievement]）。campaign 是 KPI 执行粒度。
func (c *Campaign) Achievement() float64 {
	if c.TargetCPI <= 0 || c.ActualCPI <= 0 {
		return 1 // 无目标成本或尚无转化数据 → 中性
	}
	return clampAchievement(c.TargetCPI / c.ActualCPI)
}

// DeliverTTL 返回有效的客户端展示有效期（分钟）；未配置（<=0）时兜底 DefaultDeliverTTLMinutes。
func (c *Campaign) DeliverTTL() int {
	if c.DeliverTTLMinutes <= 0 {
		return DefaultDeliverTTLMinutes
	}
	return c.DeliverTTLMinutes
}

// Snapshot 不可变配置快照：整体构建、原子替换、只读使用。
type Snapshot struct {
	Apps                  map[string]*App        // app_code → App
	AppByKeyHash          map[string]*App        // api_key_hash → App
	Advertisers           map[string]*Advertiser // advertiser_id → Advertiser
	Slots                 map[string]*Slot       // slot_code → Slot
	SlotsByKey            map[string]*Slot       // slot_key → Slot
	CreativesByAdvertiser map[string][]*Creative // advertiser_id → 活跃素材（weight 降序）
	Campaigns            map[string]*Campaign   // campaign_id → Campaign（预算闸执行粒度）
	CreativeCampaign     map[string]string      // creative_id → campaign_id（创意归属，预算闸/扣费按 campaign）
	PricingBenchmark     *PricingBenchmark      // 平台计费标准线（系统设置，全局一份）
	// Settings 运行时设置（settings 表），key → 原始 JSON；缺失时为 nil。
	// 决策缓存配置等后台可配置项在此读取，随快照热更新。
	Settings map[string]json.RawMessage // key → value(json.RawMessage)
}

// DecisionCacheConfig 决策结果缓存配置（来自 settings 表 decision_cache 行）。
type DecisionCacheConfig struct {
	Enabled    bool `json:"enabled"`     // 是否启用决策缓存
	TTLSeconds int  `json:"ttl_seconds"` // 缓存时长（秒），建议 300~600
}

// DefaultDecisionCacheConfig 未配置时的兜底：启用、5 分钟（300s）。
var DefaultDecisionCacheConfig = DecisionCacheConfig{Enabled: true, TTLSeconds: 300}

// DecisionCacheConfig 从快照的 settings 解析决策缓存配置；
// 缺失 / 非法 / ttl≤0 时兜底 DefaultDecisionCacheConfig。
func (s *Snapshot) DecisionCacheConfig() DecisionCacheConfig {
	raw, ok := s.Settings["decision_cache"]
	if !ok || len(raw) == 0 {
		return DefaultDecisionCacheConfig
	}
	var c DecisionCacheConfig
	if err := json.Unmarshal(raw, &c); err != nil {
		return DefaultDecisionCacheConfig
	}
	if c.TTLSeconds <= 0 {
		c.TTLSeconds = DefaultDecisionCacheConfig.TTLSeconds
	}
	return c
}

// FatigueConfig 用户疲劳度（全局频控）配置（来自 settings 表 fatigue 行）。
//
// 控制 1：在自定义时间窗（WindowMinutes 分钟）内，同一用户最多观看同一个
// 素材（creative）WindowMax 次；超过则在该窗口结束前对该用户自动隐藏。
//
// 控制 2：每日（滚动 24h）同一用户最多观看同一个素材 DailyMax 次。
//
// 两个上限均可后台配置（系统设置 → 全局频控配置）。
type FatigueConfig struct {
	Enabled       bool `json:"enabled"`        // 是否启用全局疲劳度控制
	WindowMinutes int  `json:"window_minutes"` // 控制1 时间窗（分钟），默认 20
	WindowMax     int  `json:"window_max"`     // 控制1 上限（该窗内最多观看次数），默认 3
	DailyMax      int  `json:"daily_max"`      // 控制2 每日上限（滚动 24h 最多观看次数），默认 8
}

// DefaultFatigueConfig 未配置时的兜底：启用、20 分钟窗内 3 次、每日 8 次。
var DefaultFatigueConfig = FatigueConfig{Enabled: true, WindowMinutes: 20, WindowMax: 3, DailyMax: 8}

// FatigueConfig 从快照的 settings 解析全局疲劳度配置；
// 缺失 / 非法 / 关键数值 ≤0 时回退到 DefaultFatigueConfig（Enabled 仍按解析结果，
// 缺省为 false，避免"写了一半的配置"误开启限制）。
func (s *Snapshot) FatigueConfig() FatigueConfig {
	raw, ok := s.Settings["fatigue"]
	if !ok || len(raw) == 0 {
		return DefaultFatigueConfig
	}
	var c FatigueConfig
	if err := json.Unmarshal(raw, &c); err != nil {
		return DefaultFatigueConfig
	}
	if c.WindowMinutes <= 0 {
		c.WindowMinutes = DefaultFatigueConfig.WindowMinutes
	}
	if c.WindowMax <= 0 {
		c.WindowMax = DefaultFatigueConfig.WindowMax
	}
	if c.DailyMax <= 0 {
		c.DailyMax = DefaultFatigueConfig.DailyMax
	}
	return c
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

// ConversionEvents 归因方 S2S 回调（/v1/s2s/event 的 event_name）支持的转化动作
// （ad_events.event_type 取值，也是 cpa_event_prices 的 key）。广告主配置 cpa
// 计费时按具体事件扣费。
// install/activate/register/first_purchase/purchase：purchase 是首充之后的
// 充值，first_purchase 是首充归因；充值事件会随回调带回 currency/value。
var ConversionEvents = []string{"install", "activate", "register", "first_purchase", "purchase"}

// ParseCPAEventPrices 解析 cpa_event_prices jsonb（转化事件 → [min,max] 单价区间美元）。
// 数组格式 [min,max]；历史单值会在 migration 000010 就地转成 [v,v]（退化为固定价）。
func ParseCPAEventPrices(b []byte) (map[string][2]float64, error) {
	if len(b) == 0 {
		return nil, nil
	}
	m := map[string][2]float64{}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// BillingAmount 按计费方式返回"该事件到达时应扣的金额"：
//
//	cpm → 仅 impression 计费，单次 = randPrice([min,max])/1000（千次价区间折算）
//	cpc → 仅 click 计费，单次 = randPrice([min,max])
//	cpa → 仅 S2S 转化事件计费，金额 = randPrice(CPAEventPrices[event] 区间)
//
// 区间在 [min,max] 内均匀随机（migration 000010）；未配置下限（min≤0 或 min≥max）
// 时退化为固定 BiddingPrice / 单值，与旧行为一致。每次扣费独立随机，实际金额以
// budget_ledger 落账为准。
//
// 返回 (金额, 是否计费)。非本模式的计费事件或金额未配置返回 (0, false)——
// 例如 cpa 广告主的 impression/click 只是过程指标，不产生扣费；
// 未配置单价的事件（如 map 缺 key）同样不扣，防止垃圾回调白白花钱。
func (a *Advertiser) BillingAmount(event string) (float64, bool) {
	if a.BiddingPrice <= 0 {
		return 0, false
	}
	switch a.BillingMode {
	case "cpm":
		if event != "impression" {
			return 0, false
		}
		return randPrice(a.BiddingPriceMin, a.BiddingPrice) / 1000, true
	case "cpc":
		if event != "click" {
			return 0, false
		}
		return randPrice(a.BiddingPriceMin, a.BiddingPrice), true
	case "cpa":
		p, ok := a.CPAEventPrices[event]
		if !ok || p[1] <= 0 {
			return 0, false
		}
		// 区间 [min,max] 内均匀随机；min≤0 或 min≥max 时 randPrice 退化为固定 max。
		return randPrice(p[0], p[1]), true
	default:
		return 0, false
	}
}

// randPrice 在 [min,max] 均匀随机；min≤0 或 min≥max 时退化为固定 max
// （未配置下限或区间非法 → 等同原固定单价，保证向后兼容）。使用 math/rand 全局
// 源（Go 1.20+ 自动 seed 且并发安全）。
func randPrice(min, max float64) float64 {
	if min <= 0 || min >= max {
		return max
	}
	return min + rand.Float64()*(max-min)
}
