package config

import "testing"

// TestPricingBenchmark_BenchmarkFor 标准线取值：按扣费方式（+CPA 事件）取对应值，
// 未配置时必须回退兜底正值——否则出价基准分计算会除零，分数直接爆炸。
func TestPricingBenchmark_BenchmarkFor(t *testing.T) {
	b := &PricingBenchmark{
		CPM: 15, CPC: 5,
		CPAInstall: 10, CPAActivate: 12, CPARegister: 15, CPAFirstPurchase: 20,
	}
	cases := []struct {
		mode, event string
		want        float64
	}{
		{"cpm", "", 15},
		{"cpc", "", 5},
		{"cpi", "", 10},
		{"cpa-activate", "", 12},
		{"cpa-register", "", 15},
		{"cpa-first-deposit", "", 20},
		{"cpa-pay", "", 25},
	}
	for _, c := range cases {
		if got := b.BenchmarkFor(c.mode, c.event); got != c.want {
			t.Errorf("BenchmarkFor(%q,%q) = %v，期望 %v", c.mode, c.event, got, c.want)
		}
	}

	// 未配置 / 未知组合：必须回退到正的兜底值，绝不能返回 0
	empty := &PricingBenchmark{}
	for _, c := range []struct{ mode, event string }{
		{"cpm", ""}, {"cpc", ""}, {"cpi", ""}, {"cpa-activate", ""},
		{"cpa-register", ""}, {"cpa-first-deposit", ""}, {"cpa-pay", ""}, {"", ""},
	} {
		if got := empty.BenchmarkFor(c.mode, c.event); got <= 0 {
			t.Errorf("空配置 BenchmarkFor(%q,%q) = %v，应回退为正的兜底值", c.mode, c.event, got)
		}
	}
	// nil 兜底
	if got := (*PricingBenchmark)(nil).BenchmarkFor("cpm", ""); got <= 0 {
		t.Errorf("nil 标准线应回退兜底正值，实际 %v", got)
	}
}

// TestPriceScore 出价基准分：计费方式 / 出价取自投放计划（campaign），
// 不同扣费维度的出价被拉平到 100 分制，逻辑与 engine.scoreCreative 一致。
func TestPriceScore(t *testing.T) {
	b := &PricingBenchmark{
		CPM: 15, CPC: 5,
		CPAInstall: 10, CPAActivate: 12, CPARegister: 15, CPAFirstPurchase: 20,
	}
	cases := []struct {
		name string
		c    *Campaign
		want float64
	}{
		// CPC 出价 6 / 标准 5 → 120 分（超过标准线，拿到优势分）
		{"CPC超标准", &Campaign{BillingMode: "cpc", BiddingPrice: 6}, 120},
		// CPI：出价 10 / 标准 CPAInstall 10 → 100 分
		{"CPI达标", &Campaign{BillingMode: "cpi", BiddingPrice: 10}, 100},
		// CPM 出价 15 / 标准 15 → 正好 100 分
		{"CPM刚好达标", &Campaign{BillingMode: "cpm", BiddingPrice: 15}, 100},
	}
	for _, c := range cases {
		price := c.c.BiddingPrice
		bench := b.BenchmarkFor(c.c.BillingMode, "")
		got := price / bench * 100
		if diff := got - c.want; diff > 1e-9 || diff < -1e-9 {
			t.Errorf("%s: 基准分 = %v，期望 %v", c.name, got, c.want)
		}
	}
}
