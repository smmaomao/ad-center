package engine

import (
	"hash/fnv"
	"math/rand/v2"
	"time"

	"adcenter/internal/config"
)

// jakarta 市场时区（与预算日一致）。
var jakarta = time.FixedZone("WIB", 7*60*60)

// score 单广告主打分（PRD 5.1），纯函数：
//
//	得分 = KPI紧急度(1/(达成率+0.01)) × KPI分档系数 × 层级权重 × 消耗节奏系数 × 填充权重
//
//	KPI 分档：达成率 > 0.85 → 达优 ×1.2；≤ 0.85 → 紧急 ×0.8
//	层级权重：Tier1=1.5 / Tier2=1.2 / Tier3=1.0
//	消耗节奏：实际进度落后预期 >10% → 偏慢 ×1.3（加速）；超前 >10% → 偏快 ×0.8（减速）
func (e *Engine) score(adv *config.Advertiser, fp config.FillPriority, now time.Time) candidate {
	c := candidate{adv: adv, fp: fp}

	c.achievement = adv.Achievement()
	c.urgency = 1 / (c.achievement + 0.01)
	if c.achievement > 0.85 {
		c.band = 1.2 // 达优：正常保障
	} else {
		c.band = 0.8 // 紧急保障（KPI 未达 85%，流量让位）
	}
	switch adv.Tier {
	case 1:
		c.tier = 1.5
	case 2:
		c.tier = 1.2
	default:
		c.tier = 1.0
	}
	c.pacing = e.pacingFactor(adv, now)

	c.score = c.urgency * c.band * c.tier * c.pacing * fp.Weight
	return c
}

// pacingFactor 消耗节奏系数（PRD 5.2 的 ±10% 偏差规则，实时版）。
func (e *Engine) pacingFactor(adv *config.Advertiser, now time.Time) float64 {
	spent, budget := e.Budget.Stats(adv.ID)
	if budget <= 0 {
		return 1
	}
	expected := dayProgress(now)
	actual := spent / budget
	switch {
	case actual < expected-0.10:
		return 1.3 // 消耗偏慢：加速
	case actual > expected+0.10:
		return 0.8 // 消耗偏快：减速
	default:
		return 1.0
	}
}

// dayProgress 当日已过时间占比（Jakarta 自然日，预算节奏基准）。
func dayProgress(now time.Time) float64 {
	j := now.In(jakarta)
	sec := float64(j.Hour()*3600 + j.Minute()*60 + j.Second())
	return sec / 86400
}

// pickCreative 从活跃素材中按权重随机选择；无可用素材返回 nil。
func pickCreative(creatives []*config.Creative, pick func(n int) int) *config.Creative {
	var active []*config.Creative
	var total float64
	for _, c := range creatives {
		if c.Status != "active" || c.Weight <= 0 {
			continue
		}
		active = append(active, c)
		total += c.Weight
	}
	if len(active) == 0 {
		return nil
	}
	// 加权随机：落点在累计权重区间
	r := float64(pick(1_000_000)) / 1_000_000.0 * total
	var cum float64
	for _, c := range active {
		cum += c.Weight
		if r < cum {
			return c
		}
	}
	return active[len(active)-1] // 浮点边界兜底
}

// defaultPick 默认随机索引选择器。
func defaultPick(n int) int {
	if n <= 0 {
		return 0
	}
	return rand.IntN(n)
}

// hashBucket 确定性分桶哈希（保底份额用）：同一输入恒定同一输出。
func hashBucket(deviceID, slotKey string, now time.Time) uint64 {
	h := fnv.New64a()
	h.Write([]byte(deviceID))
	h.Write([]byte("|"))
	h.Write([]byte(slotKey))
	h.Write([]byte("|"))
	h.Write([]byte(now.In(jakarta).Format("2006-01-02T15"))) // 小时桶
	return h.Sum64()
}
