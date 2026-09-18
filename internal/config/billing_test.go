package config

import (
	"math"
	"testing"
)

// BillingAmount 决定"事件到达时应扣多少钱"（计费执行粒度 = campaign）：
//
//	cpm → 仅 impression，randPrice(BiddingPriceMin, BiddingPrice)/1000
//	cpc → 仅 click，randPrice(BiddingPriceMin, BiddingPrice)
//	cpa → 仅 S2S 转化事件，CPAEventPrices[event]（map 缺 key 不扣）
//
// 不填下限（min≤0）→ 退化为固定 BiddingPrice（用例确定性）。
func TestCampaign_BillingAmount(t *testing.T) {
	cases := []struct {
		name       string
		camp       Campaign
		event      string
		want       float64
		wantCharge bool
	}{
		{"cpm_impression", Campaign{BillingMode: "cpm", BiddingPrice: 5.0},
			"impression", 0.005, true},
		{"cpm_click不扣", Campaign{BillingMode: "cpm", BiddingPrice: 5.0},
			"click", 0, false},
		{"cpc_click", Campaign{BillingMode: "cpc", BiddingPrice: 0.3},
			"click", 0.3, true},
		{"cpc_impression不扣", Campaign{BillingMode: "cpc", BiddingPrice: 0.3},
			"impression", 0, false},
		{"cpi_install", Campaign{BillingMode: "cpi", BiddingPrice: 1.0},
			"install", 1.0, true},
		{"cpi_activate不扣", Campaign{BillingMode: "cpi", BiddingPrice: 1.0},
			"activate", 0, false},
		{"cpa_activate事件", Campaign{BillingMode: "cpa-activate", BiddingPrice: 2.0},
			"activate", 2.0, true},
		{"cpa_pay事件", Campaign{BillingMode: "cpa-pay", BiddingPrice: 3.0},
			"purchase", 3.0, true},
		{"零单价不扣", Campaign{BillingMode: "cpm", BiddingPrice: 0}, "impression", 0, false},
		{"未知计费方式不扣", Campaign{BiddingPrice: 1.0}, "impression", 0, false},
		{"不填下限=固定单价", Campaign{BillingMode: "cpc", BiddingPrice: 0.4},
			"click", 0.4, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, charged := tc.camp.BillingAmount(tc.event)
			if charged != tc.wantCharge || (charged && math.Abs(got-tc.want) > 1e-9) {
				t.Fatalf("BillingAmount(%q) = (%v,%v)，期望 (%v,%v)",
					tc.event, got, charged, tc.want, tc.wantCharge)
			}
		})
	}
}

// nil campaign（素材未挂任务）不计费。
func TestCampaign_BillingAmount_Nil(t *testing.T) {
	var c *Campaign
	if amt, ok := c.BillingAmount("impression"); ok || amt != 0 {
		t.Fatalf("nil campaign 不应计费，得到 (%v,%v)", amt, ok)
	}
}
