package api

import (
	"slices"
	"testing"

	"adcenter/internal/config"
)

// TestNormalizeEventName 锁住 S2S 事件名归一：中介（berealads）写死的 EVENT_* 枚举
// 必须映射到服务端 ConversionEvents，否则 BillingAmount 匹配不到事件 → cpa 永不扣费。
func TestNormalizeEventName(t *testing.T) {
	cases := map[string]string{
		// 中介写死的枚举（写死在上游）
		"EVENT_INSTALL":       "install",
		"EVENT_REGISTRATION":  "register",
		"EVENT_PURCHASE":      "purchase",
		"EVENT_FIRST_DEPOSIT": "first_purchase",
		"EVENT_SUBSCRIBE":     "subscribe",
		"EVENT_APP_ACTIVATE":  "activate",
		// 已是服务端枚举 / 其它平台写法（通用兜底）
		"install":        "install",
		"Install":        "install",
		"first_purchase": "first_purchase",
		"First-Purchase": "first_purchase",
		"firstPurchase":  "first_purchase",
		"unknown_event":  "unknown_event", // 无法识别：handler 会拒绝并告警
	}
	for in, want := range cases {
		if got := normalizeEventName(in); got != want {
			t.Errorf("normalizeEventName(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestS2SEventAliasesInEnum 保证别名表所有目标值都在 ConversionEvents 内，
// 防止映射到一个不被计费/落库识别的名字。
func TestS2SEventAliasesInEnum(t *testing.T) {
	for alias, canonical := range s2sEventAliases {
		if !slices.Contains(config.ConversionEvents, canonical) {
			t.Errorf("别名 %q → %q 不在 ConversionEvents 中", alias, canonical)
		}
	}
}
