package engine

import (
	"math/rand/v2"
	"time"

	"adcenter/internal/config"
)

// jakarta 市场时区（与预算日一致）。
var jakarta = time.FixedZone("WIB", 7*60*60)

// pacingFactor 消耗节奏系数（PRD 5.2 的 ±10% 偏差规则，实时版）。spent/budget
// 为请求内预算快照（与 score 同源），不再查 Budget.Stats。
func (e *Engine) pacingFactor(adv *config.Advertiser, now time.Time, spent, budget float64) float64 {
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

// defaultPick 默认随机索引选择器。
func defaultPick(n int) int {
	if n <= 0 {
		return 0
	}
	return rand.IntN(n)
}
