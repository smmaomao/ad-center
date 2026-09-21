// Package engine 是广告决策引擎。纯内存计算、无 IO。
//
// 去广告位（slot）后模型：一次请求携带 App + 展现样式（style），引擎从
// 「target_apps 含该 App 且 styles 含该样式」的素材里筛选候选，按
// Price_Score × 优先级系数 × urgency × 消耗节奏 降序排序后下发。
//
// 分层原则（参照 Prebid AdaptedBidder 注释）：
//   - 单素材打分（Price_Score / urgency / pacing）→ score.go 纯函数
//   - 跨素材编排（筛选 / 排序 / 频控 / 兜底）→ 本文件
//
// 依赖（BudgetCtrl / FrequencyStore）通过接口注入，引擎可单测。
package engine

import (
	"log/slog"
	"sort"
	"time"

	"adcenter/internal/budget"
	"adcenter/internal/config"
	"adcenter/internal/frequency"
)

// MaxCount 单次请求条数上限（PRD 5.5）。
const MaxCount = 20

// Styles 系统支持的展现样式（client 在请求里传 style 字段，替代旧的广告位 slot_key）。
// 素材通过 Creative.Styles 声明自己支持哪些样式；引擎按此维度筛选候选。
var Styles = []string{"splash", "rewarded_video", "interstitial", "feed", "banner"}

// ValidStyle 校验请求传入的 style 是否合法。
func ValidStyle(s string) bool {
	for _, v := range Styles {
		if v == s {
			return true
		}
	}
	return false
}

// Engine 决策引擎。并发安全（无共享可变状态，依赖均为并发安全接口）。
type Engine struct {
	Freq   frequency.Store
	Budget budget.Ctrl
	// Pick 创意加权随机选择函数（索引选择器），测试注入确定性实现。
	Pick func(n int) int
}

// Request 一次决策请求（API 层已解析 App/Style 并完成校验）。
//
// 去 slot：不再有「广告位」概念，请求维度是 App + 展现样式。
type Request struct {
	App      *config.App
	Style    string // 展现样式：splash / rewarded_video / interstitial / feed / banner
	DeviceID string
	Country  string
	Language string
	Count    int
	Now      time.Time
}

// Item 一条下发结果。
type Item struct {
	AdvertiserID string           `json:"advertiser_id"`
	Advertiser   string           `json:"advertiser"`
	Creative     *config.Creative `json:"creative"`
	BidPrice     float64          `json:"bid_price"`
	Score        float64          `json:"score"`
	// MediaURL 素材下载地址（R2 预签名 GET / html 直链）。
	// 引擎不负责签名（保持纯函数），由 API 层装饰。
	// 已配 CDN 前置时为 CDN 域名 URL；未配则直连 R2（冷启动/无 CDN 自动回退）。
	MediaURL string `json:"media_url,omitempty"`
	// ExpireAt 客户端展示过期时间（Unix 秒）。超过该时间客户端应隐藏此广告，
	// 并在下次请求时重新拉取。由广告主 deliver_ttl_minutes（兜底 10 分钟）决定。
	ExpireAt int64 `json:"expire_at"`
	// CreativeHash 素材内容 SHA-256（hex）。用于客户端本地去重缓存与完整性校验。
	// 仅 video/image 下发（对象 key 即内容寻址）；html 外链不填。
	CreativeHash string `json:"creative_hash,omitempty"`
}

// Response 决策结果：Items 非空即直售填充；否则 Fallback 指示降级。
type Response struct {
	Items    []Item `json:"items"`
	Fallback string `json:"fallback,omitempty"`
}

// candidate 候选素材（引擎内部视图）。
type candidate struct {
	adv         *config.Advertiser
	creative    *config.Creative
	achievement float64 // KPI 达成率（campaign.Achievement()，目前无实测值返回中性 1.0）
	urgency     float64 // (达成率+0.01) 的倒数，越未达标越紧急
	pacing      float64 // 消耗节奏系数
	priceScore  float64 // 出价基准分（Price_Score）
	bidPrice    float64 // 出价（来自 campaign，用于 Item.BidPrice）
	score       float64
	ttl         int              // 下发有效期（分钟），来自 campaign（未配置回落默认）
	camp        *config.Campaign // 归属广告任务（任务级频控来源）
	// 请求内预算快照：buildCandidates 查一次后注入，score 与 materialize 复用。
	spent  float64
	budget float64
}

// Decide 执行决策（PRD 5.1 / 5.5，去 slot 版）。
func (e *Engine) Decide(snap *config.Snapshot, req Request) Response {
	if req.Count < 1 {
		req.Count = 1
	}
	req.Count = min(req.Count, MaxCount)

	// 前置：App 非活跃 → 直接降级
	if req.App == nil || !req.App.Active() {
		return Response{Fallback: "self_promo"}
	}

	// ① 筛选（活跃/上下架/定向/样式/投放 App）+ ② 打分
	cands := e.buildCandidates(snap, req)
	if len(cands) == 0 {
		e.diagnose(snap, req, nil)
		return Response{Fallback: "self_promo"}
	}

	// ③ 排序：得分降序，平局按 creative.ID 升序（决策可复现）
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].score != cands[j].score {
			return cands[i].score > cands[j].score
		}
		return cands[i].creative.ID < cands[j].creative.ID
	})

	// ④ 逐条物化：频控 / 预算闸挡住的跳过，至多取 count 条
	var items []Item
	matReasons := map[string]int{}
	for _, c := range cands {
		if len(items) >= req.Count {
			break
		}
		if item, ok := e.materialize(req, c); ok {
			items = append(items, item)
		} else {
			matReasons[materializeBlockedReason(e, req, c)]++
		}
	}
	if len(items) == 0 {
		e.diagnose(snap, req, matReasons)
		return Response{Fallback: "self_promo"}
	}
	return Response{Items: items}
}

// buildCandidates 遍历所有投放任务（campaign），以各任务 creative_ids 指定的素材为候选
// （素材只与投放任务挂钩，不再按广告主维度遍历全部素材）。素材经 style / 投放 App / 广告主
// 定向过滤后，按归属 campaign 的预算 / 排期 / KPI 打分；未关联任何任务的素材不参与投放。
func (e *Engine) buildCandidates(snap *config.Snapshot, req Request) []candidate {
	campBudgets := make(map[string][2]float64, len(snap.Campaigns))
	for id := range snap.Campaigns {
		spent, budget := e.Budget.Stats(id)
		campBudgets[id] = [2]float64{spent, budget}
	}
	cands := make([]candidate, 0)
	for _, camp := range snap.Campaigns {
		if !camp.Active(req.Now) {
			continue // 任务暂停或超出投放排期
		}
		adv := snap.Advertisers[camp.AdvertiserID]
		if adv == nil || !adv.Active(req.Now) {
			continue
		}
		if !adv.Targeting.Match(req.Country, req.Language) {
			continue
		}
		spent, budget := 0.0, 0.0
		if b, ok := campBudgets[camp.ID]; ok {
			spent, budget = b[0], b[1]
		}
		// 候选仅来自本任务关联的素材（白名单）：素材不与广告主挂钩，只与任务挂钩。
		for _, crID := range camp.CreativeIDs {
			cr := snap.CreativesByID[crID]
			if cr == nil || !creativeActive(cr) {
				continue
			}
			if !styleIn(cr.Styles, req.Style) {
				continue
			}
			if !targetsApp(cr.TargetApps, req.App.ID) {
				continue
			}
			cands = append(cands, e.scoreCreative(cr, adv, camp, snap.PricingBenchmark, req.Now, spent, budget))
		}
	}
	return cands
}

// creativeActive 素材是否处于可投放状态（排期由归属的 campaign 控制，见 buildCandidates）。
func creativeActive(c *config.Creative) bool {
	return c.Status == "active" || c.Status == "testing"
}

func styleIn(styles []string, style string) bool {
	for _, s := range styles {
		if s == style {
			return true
		}
	}
	return false
}

// targetsApp 素材是否投放到该 App：target_apps 为空 = 投放全部 App。
func targetsApp(targetApps []string, appID string) bool {
	if len(targetApps) == 0 {
		return true
	}
	for _, a := range targetApps {
		if a == appID {
			return true
		}
	}
	return false
}

// scoreCreative 单素材打分：
//
//	Price_Score = 投放计划出价 ÷ 平台计费标准线 × 100
//	score       = Price_Score × 优先级系数(priority_score) × urgency × 消耗节奏
//
// 出价 / 计费方式 / 优先级系数 均取自归属的广告任务（campaign），素材只承载资产属性。
func (e *Engine) scoreCreative(
	c *config.Creative, adv *config.Advertiser, camp *config.Campaign,
	bm *config.PricingBenchmark, now time.Time, spent, budget float64,
) candidate {
	// KPI 达成率来自广告任务（campaign）；未归属任务的素材按中性 1.0 参与排序。
	// 注意：当前 Campaign.Achievement() 恒返中性 1.0（实时出价/自动优化策略尚未实现，
	// 见 config/types.go 注释与 docs/PLAN.md），故 urgency 目前是常数 ~0.99，
	// KPI 紧急度分支处于「已接入但中立」状态；策略落地后此处自动恢复按真实达成率区分。
	achievement := 1.0
	if camp != nil {
		achievement = camp.Achievement()
	}
	urgency := 1.0 / (achievement + 0.01) // 纯达成率驱动（Tier 层权重已移除）；当前因达成率中性而恒为常数
	pacing := e.pacingFactor(adv, now, spent, budget)

	// 出价基准分：计费方式 / 出价来自投放计划（campaign），用平台标准线拉平到 100 分制比较。
	// 计费方式 → 标准线由 BenchmarkFor 按 billing_mode 取；未挂 campaign 的素材按 cpm 中性基准。
	var benchmark float64
	price := 0.0
	if camp != nil {
		price = camp.BiddingPrice
		benchmark = bm.BenchmarkFor(camp.BillingMode, "")
	} else {
		benchmark = bm.BenchmarkFor("cpm", "")
	}
	priceScore := (price / benchmark) * 100

	// 下发有效期来自广告任务（campaign），未配置回落默认值
	ttl := config.DefaultDeliverTTLMinutes
	if camp != nil {
		ttl = camp.DeliverTTL()
	}

	// 优先级系数改读 campaign 的 priority_score（替代原素材 weight）
	weight := 1.0
	if camp != nil && camp.PriorityScore > 0 {
		weight = camp.PriorityScore
	}

	score := priceScore * weight * urgency * pacing
	return candidate{
		adv:         adv,
		creative:    c,
		achievement: achievement,
		urgency:     urgency,
		pacing:      pacing,
		priceScore:  priceScore,
		bidPrice:    price,
		score:       score,
		ttl:         ttl,
		camp:        camp,
		spent:       spent,
		budget:      budget,
	}
}

// CampaignFreqWindows 由 campaign 的配置推导频控窗口（统一给 materialize 的
// Check 与 api 层 impression 的 Record 用，保证"只读检查"与"真实观看计数"用同一套
// 窗口定义）：
//   - freq_interval_minutes（>0 且 freq_fatigue_window>0）= 滑动窗口长度（分钟）
//   - freq_fatigue_window = 该窗口内最大下发次数
//   - 另叠加 24h 日频控 freq_daily_limit（>0 时生效）
//
// 任一窗口为 0 则该档视为不限。
func CampaignFreqWindows(camp *config.Campaign) []frequency.Window {
	if camp == nil {
		return nil
	}
	var windows []frequency.Window
	if camp.FreqIntervalMinute > 0 && camp.FreqFatigueWindow > 0 {
		windows = append(windows, frequency.Window{
			WindowMinutes: camp.FreqIntervalMinute,
			MaxCount:      camp.FreqFatigueWindow,
		})
	}
	if camp.FreqDailyLimit > 0 {
		windows = append(windows, frequency.Window{
			WindowMinutes: 1440,
			MaxCount:      camp.FreqDailyLimit,
		})
	}
	return windows
}

// materialize 单条物化：任务级滑动窗口频控（app:device:campaign）→ 预算只读闸。
//
// 注意：频控的"计数"不在这里做，计数发生在用户真实观看（impression）时由 api
// 层调用 frequency.Store.Record（见 CampaignFreqWindows 的同款窗口）。materialize
// 只通过 frequency.Store.Check 读取计数，据此隐藏已达上限的素材——决策缓存命中
// 也不会误增计数，上限始终以真实观看次数为准。
func (e *Engine) materialize(req Request, c candidate) (Item, bool) {
	// 任务级滑动窗口频控（每个 campaign 独立；广告主级 freq_windows 已弃用）。
	// 计数时机改为真实观看（impression），此处仅做只读检查。
	if c.camp != nil {
		windows := CampaignFreqWindows(c.camp)
		if len(windows) > 0 {
			if !e.Freq.Check(req.App.ID, req.DeviceID, c.creative.ID, c.camp.ID,
				frequency.AdvPolicy{Windows: windows}, req.Now) {
				return Item{}, false
			}
		}
	}
	// 广告主总钱包硬顶：余额 <=0 → 停投该广告主下全部 campaign（即使 campaign
	// 日预算没超）。未启用钱包的广告主 WalletBalance 返回 +inf，不受此闸限制。
	if c.adv != nil && e.Budget.WalletBalance(c.adv.ID) <= 0 {
		return Item{}, false
	}
	// 预算只读闸：用请求内快照判断（spent >= budget 停投，不扣费）。
	if c.budget > 0 && c.spent >= c.budget {
		return Item{}, false
	}
	return Item{
		AdvertiserID: c.adv.ID,
		Advertiser:   c.adv.Name,
		Creative:     c.creative,
		BidPrice:     c.bidPrice,
		Score:        c.score,
		ExpireAt:     req.Now.Add(time.Duration(c.ttl) * time.Minute).Unix(),
	}, true
}

// diagnose 在决策落空（无填充）时打印各阶段过滤计数，定位空列表根因。
// 仅在 Decide 返回 fallback 时调用，正常填充不产生日志。
func (e *Engine) diagnose(snap *config.Snapshot, req Request, mat map[string]int) {
	reasons := map[string]int{}
	campTotal := len(snap.Campaigns)
	crTotal := len(snap.CreativesByID)
	advTotal := len(snap.Advertisers)
	for _, camp := range snap.Campaigns {
		if !camp.Active(req.Now) {
			reasons["campaign_inactive_or_schedule"]++
			continue
		}
		adv := snap.Advertisers[camp.AdvertiserID]
		if adv == nil {
			reasons["advertiser_nil"]++
			continue
		}
		if !adv.Active(req.Now) {
			reasons["advertiser_inactive"]++
			continue
		}
		if !adv.Targeting.Match(req.Country, req.Language) {
			reasons["targeting_miss"]++
			continue
		}
		for _, crID := range camp.CreativeIDs {
			cr := snap.CreativesByID[crID]
			if cr == nil {
				reasons["creative_nil"]++
				continue
			}
			if !creativeActive(cr) {
				reasons["creative_inactive"]++
				continue
			}
			if !styleIn(cr.Styles, req.Style) {
				reasons["style_miss"]++
				continue
			}
			if !targetsApp(cr.TargetApps, req.App.ID) {
				reasons["app_miss"]++
				continue
			}
			reasons["passed"]++
		}
	}
	slog.Info("ad/list diagnose: no fill",
		"app", req.App.ID, "style", req.Style, "app_active", req.App.Active(),
		"country", req.Country, "language", req.Language,
		"campaigns_total", campTotal, "creatives_total", crTotal, "advertisers_total", advTotal,
		"build_reasons", reasons, "materialize_reasons", mat,
	)
}

// materializeBlockedReason 返回某候选在物化阶段被挡的原因（只读，不影响状态）。
func materializeBlockedReason(e *Engine, req Request, c candidate) string {
	if c.camp != nil {
		windows := CampaignFreqWindows(c.camp)
		if len(windows) > 0 {
			if !e.Freq.Check(req.App.ID, req.DeviceID, c.creative.ID, c.camp.ID,
				frequency.AdvPolicy{Windows: windows}, req.Now) {
				return "freq"
			}
		}
	}
	if c.adv != nil && e.Budget.WalletBalance(c.adv.ID) <= 0 {
		return "wallet_zero"
	}
	if c.budget > 0 && c.spent >= c.budget {
		return "budget_exhausted"
	}
	return "other"
}
