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
	ID             string `json:"id"`
	Name           string `json:"name"`
	APIKeyHash     string `json:"-"`                      // sha256 hex，用于 X-Api-Key 匹配
	Status         string `json:"status"`                 // active / paused
	CallbackURL    string `json:"callback_url,omitempty"` // 业务后端 S2S 接收地址（激励视频完播回调用）
	AdWatchParams  string `json:"ad_watch_params,omitempty"` // 激励视频完播回传业务后端的附加参数（JSON 对象，扩展用，如鉴权）
	AddServerID    string `json:"add_server_id,omitempty"`   // app 服务端侧的应用 id（转发 reward 回调时作为 app_id 发送）
}

// Active 报告该 App 是否可服务。
func (a *App) Active() bool { return a.Status == "active" }

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

// Advertiser 广告主（投放主体，全局共享：素材跨 App 一份）。
//
// 职责收敛：广告主只承载「身份 + 状态 + 定向 + 钱包（余额 / 充值扣费流水）」。
// 出价 / 计费方式 / CPA 单价 / KPI / 排期 / 频控 / 下发有效期等**所有投放与计费
// 公式一律在广告任务（campaign）级**，广告主不参与任何排序或扣费计算。
type Advertiser struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Status      string       `json:"status"` // active / paused / budget_exhausted
	Targeting Targeting `json:"targeting"`
}

// Active 报告广告主账户当前是否可参排（状态活跃）。投放结束日期 / 排期由广告任务
// （campaign）控制，见 Campaign.Active。
func (a *Advertiser) Active(now time.Time) bool {
	return a.Status == "active"
}

// DefaultDeliverTTLMinutes 广告下发到客户端的默认展示有效期（分钟）。
// campaign 未配置 deliver_ttl_minutes（<=0）时采用此兜底值。
const DefaultDeliverTTLMinutes = 10

// KPI 达成率口径（PRD 6.1 / FR-02 / FR-08）：达成率 = target_kpi_value / 实测值。
// 实测 CPI 不再落库，目前无实测值可比对 → 中性 1.0；后期自动优化时按需以运行时实测值替换。
// 该口径由广告任务（campaign，KPI 执行粒度）持有，见 Campaign.Achievement()；
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
	ID           string `json:"id"`
	AdvertiserID string `json:"advertiser_id"`
	Name         string `json:"name"`
	MediaType    string `json:"media_type"`   // video / image / html
	StoragePath  string `json:"storage_path"` // R2 对象 key 或完整 URL（html）
	Orientation  string `json:"orientation"`  // portrait / landscape / square / any
	Width        int    `json:"width,omitempty"`
	Height       int    `json:"height,omitempty"`
	DurationMS   int    `json:"duration_ms,omitempty"`
	Status       string `json:"status"` // testing / active / paused
	ABGroup      string `json:"ab_group,omitempty"`

	// 展现样式（多选）：splash 开屏 / rewarded_video 激励视频 / interstitial 插屏
	// / feed 信息流 / banner 横幅。素材直接声明支持哪些样式，不再依赖 slot。
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
	CPAPurchase      float64 `json:"cpa_purchase"`
}

// DefaultPricingBenchmark 标准线未配置时的兜底值（避免除零导致基准分爆炸）。
func DefaultPricingBenchmark() *PricingBenchmark {
	return &PricingBenchmark{
		CPM: 15, CPC: 5,
		CPAInstall: 10, CPAActivate: 12, CPARegister: 15, CPAFirstPurchase: 20, CPAPurchase: 25,
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
	case "cpi":
		v = b.CPAInstall
	case "cpa-activate":
		v = b.CPAActivate
	case "cpa-register":
		v = b.CPARegister
	case "cpa-first-deposit":
		v = b.CPAFirstPurchase
	case "cpa-pay":
		v = b.CPAPurchase
	}
	if v <= 0 {
		// 该项没配 → 用兜底，避免除零
		d := DefaultPricingBenchmark()
		switch billingMode {
		case "cpm":
			return d.CPM
		case "cpc":
			return d.CPC
		case "cpi":
			return d.CPAInstall
		case "cpa-activate":
			return d.CPAActivate
		case "cpa-register":
			return d.CPARegister
		case "cpa-first-deposit":
			return d.CPAFirstPurchase
		case "cpa-pay":
			return d.CPAPurchase
		default:
			return d.CPC
		}
	}
	return v
}

// Campaign 广告任务（投放执行粒度）。预算闸与扣费按 campaign 各自控制，
// 因此 snapshot 同时持有 Campaigns 与 creative→campaign 归属映射。
type Campaign struct {
	ID                 string                `json:"id"`
	AdvertiserID       string                `json:"advertiser_id"`
	Name               string                `json:"name"`
	Status             string                `json:"status"` // active / paused
	DailyBudget        float64               `json:"daily_budget"`
	SpentToday         float64               `json:"spent_today"`
	ConsumeSpeed       int                   `json:"consume_speed"` // 曝光系数 1-10（默认 5）：影响下发节奏
	TargetKPIType      string                `json:"target_kpi_type"` // KPI 指标类型（与 billing_mode 同枚举，通常与其一致）
	TargetKPIValue     float64               `json:"target_kpi_value"` // 目标 KPI 值（如目标 CPI=$1.80）
	BillingMode        string                `json:"billing_mode"`                // cpm / cpc / cpa（唯一计费方式：决定按什么事件扣费 + 打分标准线）
	BiddingPrice       float64               `json:"bidding_price"`               // 出价（该计费方式的单价上限；cpm=每千次、cpc/cpa=每事件）
	BiddingPriceMin    float64               `json:"bidding_price_min,omitempty"` // 出价下限：0=固定单价(=BiddingPrice)，>0 则在 [min,max] 随机
	CPAEventPrices     map[string][2]float64 `json:"cpa_event_prices,omitempty"`  // cpa 计费：事件→[min,max] 单价区间（按 S2S 回调事件扣费）
	PriorityScore      float64               `json:"priority_score"`              // 优先级系数（替代原素材 weight）
	DeliverTTLMinutes  int                   `json:"deliver_ttl_minutes"`
	CreativeIDs        []string              `json:"creative_ids"`
	StartAt            *time.Time            `json:"start_at,omitempty"`
	EndAt              *time.Time            `json:"end_at,omitempty"`
	LandingURL         string                `json:"landing_url,omitempty"` // 落地页 URL（click_url 来源）
	FreqDailyLimit     int                   `json:"freq_daily_limit"`      // 每日上限（滚动 24h 内最多下发次数，0 = 不限）
	FreqIntervalMinute int                   `json:"freq_interval_minutes"` // 滑动窗口长度（分钟，0 = 不启用该窗口）
	FreqFatigueWindow  int                   `json:"freq_fatigue_window"`   // 窗口内上限（该窗口内最多下发同一任务的次数，0 = 不限）
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

// Achievement 计算 KPI 达成率（PRD 6.1 / FR-02 / FR-08）：达成率 = target_kpi_value / 实测值。
//
// 字段状态：target_kpi_type / target_kpi_value 已落库（迁移 000051）并可在后台配置，
// 但「基于实测 CPI 的实时出价 / 自动优化策略」本期【不实现】，推迟到后期（见 docs/PLAN.md
// 「P1 待办 · KPI 自动优化」）。因此此处不持有任何运行时实测值（actual_cpi 已不落库，迁移 000052），
// 达成率一律返回中性 1.0 —— 引擎的 KPI 紧急度分支也随之中立，不影响现有排序
// （价格得分 × 优先级系数 × 消耗节奏 仍正常生效）。
//
// 后期接入时：在此引入运行时实测值（消耗/转化按 target_kpi_type 口径归集），
// 用 target_kpi_value / 实测值 计算真实达成率，再驱动 engine.urgency；
// 届时本函数与 engine.go 的 urgency 分支会自动恢复为按真实达成率区分。
func (c *Campaign) Achievement() float64 {
	return 1 // 中性：实时出价/自动优化策略尚未实现（见上注释与 docs/PLAN.md）
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
	CreativesByAdvertiser map[string][]*Creative // advertiser_id → 活跃素材（保留：素材管理/诊断；投放候选不再按广告主遍历）
	CreativesByID         map[string]*Creative  // creative_id → Creative（投放候选按 ID 命中任务 creative_ids）
	Campaigns             map[string]*Campaign   // campaign_id → Campaign（预算闸执行粒度）
	CreativeCampaign      map[string]string      // creative_id → campaign_id（创意归属，预算闸/扣费按 campaign）
	PricingBenchmark      *PricingBenchmark      // 平台计费标准线（系统设置，全局一份）
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
	// 用户要求：所有 Redis key 过期 ≤ 7 天，决策缓存 TTL 上限收紧到 7 天（604800s）。
	const maxTTLSeconds = 7 * 24 * 3600
	if c.TTLSeconds > maxTTLSeconds {
		c.TTLSeconds = maxTTLSeconds
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

// ConversionEvents 归因方 S2S 回调（/v1/s2s/event 的 event_name）支持的转化动作
// （ad_conversions.event_type 取值）。广告主配置 cpa 计费时按具体事件扣费
// （BillingAmount：cpi→install、cpa-activate→activate、cpa-register→register、
// cpa-first-deposit→first_purchase、cpa-pay→purchase）。
// purchase 是首充之后的充值，first_purchase 是首充归因；subscribe 为订阅事件
// （当前无对应计费方式，仅记录不扣费）。充值事件会随回调带回 currency/value。
var ConversionEvents = []string{"install", "activate", "register", "first_purchase", "purchase", "subscribe"}

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

// BillingAmount 按计费方式返回"该事件到达时应扣的金额"（计费执行粒度 = campaign）：
//
//	cpm               → impression 到达即扣 BiddingPrice/1000（真实曝光才花钱）
//	cpc               → click 到达即扣 BiddingPrice
//	cpi               → S2S install 转化即扣 BiddingPrice
//	cpa-activate      → S2S activate 转化即扣 BiddingPrice
//	cpa-register      → S2S register 转化即扣 BiddingPrice
//	cpa-first-deposit → S2S first_purchase 转化即扣 BiddingPrice
//	cpa-pay           → S2S purchase 转化即扣 BiddingPrice
//
// 单价语义（与后台表单一致）：BiddingPrice 是该计费方式的单价上限，BiddingPriceMin
// 是下限；填了下限就在 [min,max] 内均匀随机，不填（min≤0 或 min≥max）则退化为固定
// BiddingPrice。每次扣费独立随机，实际金额以 budget_ledger 落账为准。
//
// 返回 (金额, 是否计费)。非本模式的计费事件（如 cpm 任务的 click）返回 (0, false)，
// 防止垃圾回调白白花钱；camp 为 nil（素材未挂任务）时不计费。
func (c *Campaign) BillingAmount(event string) (float64, bool) {
	if c == nil || c.BiddingPrice <= 0 {
		return 0, false
	}
	// 计费方式 → 计费事件：cpm/cpc 是客户端回执，cpi/cpa-* 是 S2S 转化事件。
	var bill string
	switch c.BillingMode {
	case "cpm":
		bill = "impression"
	case "cpc":
		bill = "click"
	case "cpi":
		bill = "install"
	case "cpa-activate":
		bill = "activate"
	case "cpa-register":
		bill = "register"
	case "cpa-first-deposit":
		bill = "first_purchase"
	case "cpa-pay":
		bill = "purchase"
	default:
		return 0, false
	}
	if event != bill {
		return 0, false
	}
	amt := randPrice(c.BiddingPriceMin, c.BiddingPrice)
	if c.BillingMode == "cpm" {
		amt /= 1000 // 千次曝光价折算到单次
	}
	return amt, true
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
