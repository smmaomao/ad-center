package config

import (
	"math"
	"testing"
)

// BillingAmount 决定"事件到达时应扣多少钱"（migration 000008 计费模型）：
//
//	cpm → 仅 impression，BiddingPrice/1000
//	cpc → 仅 click，BiddingPrice
//	cpa → 仅 S2S 转化事件，CPAEventPrices[event]（map 缺 key 不扣）
func TestAdvertiser_BillingAmount(t *testing.T) {
	cases := []struct {
		name       string
		adv        Advertiser
		event      string
		want       float64
		wantCharge bool
	}{
		{"cpm_impression", Advertiser{BillingMode: "cpm", BiddingPrice: 5.0},
			"impression", 0.005, true},
		{"cpm_click不扣", Advertiser{BillingMode: "cpm", BiddingPrice: 5.0},
			"click", 0, false},
		{"cpc_click", Advertiser{BillingMode: "cpc", BiddingPrice: 0.3},
			"click", 0.3, true},
		{"cpc_impression不扣", Advertiser{BillingMode: "cpc", BiddingPrice: 0.3},
			"impression", 0, false},
		{"cpa_install", Advertiser{BillingMode: "cpa", BiddingPrice: 1.0,
			CPAEventPrices: map[string][2]float64{"install": {1.0, 1.0}}}, "install", 1.0, true},
		{"cpa_未配置事件不扣", Advertiser{BillingMode: "cpa", BiddingPrice: 1.0,
			CPAEventPrices: map[string][2]float64{"install": {1.0, 1.0}}}, "activate", 0, false},
		{"cpa_空map不扣", Advertiser{BillingMode: "cpa", BiddingPrice: 1.0},
			"install", 0, false},
		{"零单价不扣", Advertiser{BillingMode: "cpm", BiddingPrice: 0}, "impression", 0, false},
		{"未知计费方式不扣", Advertiser{BiddingPrice: 1.0}, "impression", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, charged := tc.adv.BillingAmount(tc.event)
			if charged != tc.wantCharge || (charged && math.Abs(got-tc.want) > 1e-9) {
				t.Fatalf("BillingAmount(%q) = (%v,%v)，期望 (%v,%v)",
					tc.event, got, charged, tc.want, tc.wantCharge)
			}
		})
	}
}
