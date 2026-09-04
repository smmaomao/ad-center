// Package engine 是广告决策引擎（PRD 5.1 单条 + 5.5 批量），纯内存计算、无 IO。
//
// 分层原则（参照 Prebid AdaptedBidder 注释）：
//   - 单广告主内的计算（打分/分档/消耗进度）→ score.go 纯函数
//   - 跨广告主逻辑（保底/排序/名单/兜底链）→ 本文件编排层
//
// 依赖（BudgetCtrl / FrequencyStore）通过接口注入，引擎可单测、可基准测试。
package engine

import (
	"cmp"
	"math"
	"slices"
	"sort"
	"time"

	"adcenter/internal/budget"
	"adcenter/internal/config"
	"adcenter/internal/frequency"
)

// MaxCount 单次请求条数上限（PRD 5.5）。
const MaxCount = 20

// Engine 决策引擎。并发安全（无共享可变状态，依赖均为并发安全接口）。
type Engine struct {
	Freq   frequency.Store
	Budget budget.Ctrl
	// Pick 创意加权随机选择函数（索引选择器），测试注入确定性实现。
	Pick func(n int) int
}

// Request 一次决策请求（API 层已解析 App/Slot 并完成校验）。
type Request struct {
	App      *config.App
	Slot     *config.Slot
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
}

// Response 决策结果：Items 非空即直售填充；否则 Fallback 指示降级链
// （"max" = 客户端走 MAX 聚合，"self_promo" = 平台自有推广）。
type Response struct {
	Items    []Item `json:"items"`
	Fallback string `json:"fallback,omitempty"`
}

// candidate 候选广告主（引擎内部视图）。
type candidate struct {
	adv *config.Advertiser
	fp  config.FillPriority
	// 打分中间量（请求内一次计算，多处复用）
	achievement float64
	urgency     float64
	band        float64 // KPI 分档系数
	tier        float64 // 层级权重
	pacing      float64 // 消耗节奏系数
	score       float64
}

// Decide 执行决策（PRD 5.1 / 5.5）。
func (e *Engine) Decide(snap *config.Snapshot, req Request) Response {
	if req.Count < 1 {
		req.Count = 1
	}
	req.Count = min(req.Count, MaxCount)

	// 前置：广告位/App 非活跃 → 直接降级
	if req.App == nil || !req.App.Active() || req.Slot == nil || !req.Slot.Active() {
		return e.fallback(req.Slot)
	}

	// ① 筛选 + ②③ 打分（候选构建，纯函数见 score.go）
	cands := e.buildCandidates(snap, req)
	if len(cands) == 0 {
		return e.fallback(req.Slot)
	}

	// ④ 排序：得分降序，平局按 advertiser_id 升序（决策可复现）
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].score != cands[j].score {
			return cands[i].score > cands[j].score
		}
		return cands[i].adv.ID < cands[j].adv.ID
	})

	// ⑤ 广告位级额度预检（count 条一起判，批量不互相卡间隔）
	slotPolicy := frequency.SlotPolicy{
		Interval:   time.Duration(req.Slot.FreqIntervalMinutes) * time.Minute,
		DailyLimit: req.Slot.FreqDailyLimit,
	}
	if !e.Freq.CheckSlot(req.App.ID, req.DeviceID, req.Slot.ID, slotPolicy, req.Count, req.Now) {
		return e.fallback(req.Slot)
	}

	// ⑥ 名单生成 + 逐条物化
	var items []Item
	if req.Count == 1 {
		items = e.decideSingle(snap, req, cands)
	} else {
		items = e.decideBatch(snap, req, cands)
	}

	if len(items) == 0 {
		return e.fallback(req.Slot)
	}
	e.Freq.RecordSlot(req.App.ID, req.DeviceID, req.Slot.ID, len(items), req.Now)
	return Response{Items: items}
}

// buildCandidates 筛选（活跃/截止/定向/有可投素材）+ 打分。
func (e *Engine) buildCandidates(snap *config.Snapshot, req Request) []candidate {
	cands := make([]candidate, 0, len(req.Slot.Priorities))
	for _, fp := range req.Slot.Priorities {
		if fp.SourceType != "advertiser" || fp.Weight <= 0 || fp.AdvertiserID == "" {
			continue
		}
		adv, ok := snap.Advertisers[fp.AdvertiserID]
		if !ok || !adv.Active(req.Now) {
			continue
		}
		if !adv.Targeting.Match(req.Country, req.Language) {
			continue
		}
		if !hasActiveCreative(snap, adv.ID) {
			continue
		}
		cands = append(cands, e.score(adv, fp, req.Now))
	}
	return cands
}

func hasActiveCreative(snap *config.Snapshot, advertiserID string) bool {
	for _, c := range snap.CreativesByAdvertiser[advertiserID] {
		if c.Status == "active" {
			return true
		}
	}
	return false
}

// decideSingle 单条模式：保底桶优先，按排序顺延物化（PRD 5.1 疲劳过滤 = 跳过继续）。
func (e *Engine) decideSingle(snap *config.Snapshot, req Request, cands []candidate) []Item {
	ordered := slices.Clone(cands)

	// 保底：确定性分桶（设备+广告位+小时），流量份额按小时逼近
	if pick, ok := guaranteedPick(ordered, req); ok {
		ordered = reorderFirst(ordered, pick)
	}

	for _, c := range ordered {
		if item, ok := e.materialize(snap, req, c); ok {
			return []Item{item}
		}
	}
	return nil
}

// decideBatch 批量模式（PRD 5.5）：
// 得分定名单（保底 ceil 强制换入）→ 整轮下发（轮内单价降序）→ 余数给高分者
// → 逐条物化，失败跳过不补位。
func (e *Engine) decideBatch(snap *config.Snapshot, req Request, cands []candidate) []Item {
	n := req.Count
	l := min(len(cands), n) // 名单大小：分数 top-l（保底可强制换入）
	quota := make([]int, len(cands))
	rounds, rem := n/l, n%l
	for i := range quota {
		if i >= l {
			break // 名单外的候选基础额度为 0
		}
		quota[i] = rounds
		if i < rem {
			quota[i]++
		}
	}

	// 保底强制换入：ceil(n × share) 不够则从最低分的非保底候选挪额度
	for i := range cands {
		if share := cands[i].fp.GuaranteedShare; share > 0 {
			want := int(math.Ceil(float64(n) * share))
			for quota[i] < want {
				stolen := false
				// 从最低分的非保底候选挪（保底者自身可能就在末位，需全表扫描）
				for j := len(cands) - 1; j >= 0; j-- {
					if j != i && cands[j].fp.GuaranteedShare <= 0 && quota[j] > 0 {
						quota[j]--
						quota[i]++
						stolen = true
						break
					}
				}
				if !stolen {
					break // 无处可挪（全部是保底或额度已空）
				}
			}
		}
	}

	// 整轮下发：每轮包含所有尚有额度的候选，轮内按出价降序（同价按得分）
	var items []Item
	for {
		round := make([]int, 0, l)
		for i := range cands {
			if quota[i] > 0 {
				round = append(round, i)
			}
		}
		if len(round) == 0 {
			break
		}
		slices.SortStableFunc(round, func(a, b int) int {
			if cands[a].adv.BiddingPrice != cands[b].adv.BiddingPrice {
				return cmp.Compare(cands[b].adv.BiddingPrice, cands[a].adv.BiddingPrice)
			}
			return cmp.Compare(cands[b].score, cands[a].score)
		})
		for _, i := range round {
			quota[i]--
			if item, ok := e.materialize(snap, req, cands[i]); ok {
				items = append(items, item)
			}
			// 物化失败（频控/预算/无素材）：跳过不补位
		}
	}
	return items
}

// materialize 单条物化：频控 → 素材选择 → 预算预扣。任一步失败即放弃该条。
func (e *Engine) materialize(snap *config.Snapshot, req Request, c candidate) (Item, bool) {
	advPolicy := frequency.AdvPolicy{
		Windows:  toFreqWindows(c.adv.FreqWindows),
		FatigueN: req.Slot.FreqFatigueWindow,
	}
	if !e.Freq.CheckAndIncr(req.App.ID, req.DeviceID, req.Slot.ID, c.adv.ID, advPolicy, req.Now) {
		return Item{}, false
	}
	creative := pickCreative(snap.CreativesByAdvertiser[c.adv.ID], e.pickFunc())
	if creative == nil {
		return Item{}, false
	}
	if !e.Budget.TryDeduct(c.adv.ID, c.adv.BiddingPrice) {
		return Item{}, false
	}
	return Item{
		AdvertiserID: c.adv.ID,
		Advertiser:   c.adv.Name,
		Creative:     creative,
		BidPrice:     c.adv.BiddingPrice,
		Score:        c.score,
	}, true
}

func (e *Engine) pickFunc() func(n int) int {
	if e.Pick != nil {
		return e.Pick
	}
	return defaultPick
}

// fallback 兜底链：无直售填充时按广告位配置指示降级（MAX → 平台自有）。
func (e *Engine) fallback(slot *config.Slot) Response {
	r := Response{Fallback: "self_promo"}
	if slot != nil {
		for _, fp := range slot.Priorities {
			if fp.SourceType == "max" {
				r.Fallback = "max"
				break
			}
		}
	}
	return r
}

// guaranteedPick 保底分桶：bucket = hash(deviceID|slotKey|小时桶) ∈ [0,10000)，
// 命中某保底候选的份额区间则优先它。小时级分桶使份额按小时逼近配置值，
// 同一设备跨小时有轮换（避免锁定单一来源）。
func guaranteedPick(cands []candidate, req Request) (int, bool) {
	var total float64
	for _, c := range cands {
		total += c.fp.GuaranteedShare
	}
	if total <= 0 {
		return 0, false
	}
	bucket := hashBucket(req.DeviceID, req.Slot.Key, req.Now) % 10000
	var cum float64
	// 保底候选按得分降序覆盖份额区间
	for i, c := range cands {
		if c.fp.GuaranteedShare <= 0 {
			continue
		}
		cum += c.fp.GuaranteedShare * 10000
		if float64(bucket) < cum {
			return i, true
		}
	}
	return 0, false
}

// reorderFirst 把索引 i 的候选提到队首（其余保持原序）。
func reorderFirst(cands []candidate, i int) []candidate {
	out := make([]candidate, 0, len(cands))
	out = append(out, cands[i])
	out = append(out, cands[:i]...)
	return append(out, cands[i+1:]...)
}

func toFreqWindows(ws []config.FreqWindow) []frequency.Window {
	if len(ws) == 0 {
		return nil
	}
	out := make([]frequency.Window, len(ws))
	for i, w := range ws {
		out[i] = frequency.Window{WindowMinutes: w.WindowMinutes, MaxCount: w.MaxCount}
	}
	return out
}
