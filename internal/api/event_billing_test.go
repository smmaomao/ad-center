package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"math"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"adcenter/internal/budget"
	"adcenter/internal/config"
	"adcenter/internal/engine"
	"adcenter/internal/metrics"
	"adcenter/internal/queue"
)

// 计费模型集成测试（migration 000008）：
// 扣费只发生在真实计费事件，金额与锚点由 advertiser.billing_mode 决定：
//   cpm → impression（BiddingPrice/1000）；cpc → click；cpa → S2S 转化事件。
// 客户端通道不再接受 conversion；金额由服务端计算，不信上报。

func billTestServer(t *testing.T, adv *config.Advertiser, balances map[string][2]float64) *Server {
	t.Helper()
	const apiKey = "adc_test_0000000000000000000000000001"
	sum := sha256.Sum256([]byte(apiKey))
	keyHash := hex.EncodeToString(sum[:])

	snap := &config.Snapshot{
		Apps:                  map[string]*config.App{},
		AppByKeyHash:          map[string]*config.App{},
		Advertisers:           map[string]*config.Advertiser{},
		Slots:                 map[string]*config.Slot{},
		SlotsByKey:            map[string]*config.Slot{},
		CreativesByAdvertiser: map[string][]*config.Creative{},
		Campaigns:             map[string]*config.Campaign{},
		CreativeCampaign:      map[string]string{},
	}
	app := &config.App{ID: "app_t", Status: "active"}
	snap.Apps[app.ID] = app
	snap.AppByKeyHash[keyHash] = app
	slot := &config.Slot{ID: "slot_t", AppID: app.ID, Key: "test_slot", Status: "active"}
	snap.Slots[slot.ID] = slot
	snap.SlotsByKey[slot.Key] = slot
	snap.Advertisers[adv.ID] = adv
	snap.CreativesByAdvertiser[adv.ID] = []*config.Creative{{
		ID: "cr_t", AdvertiserID: adv.ID, MediaType: "video", Status: "active",
	}}
	// 扣费按 campaign 执行（与引擎一致）：用 advertiserID 作 campaignID，
	// 既建立 creative→campaign 归属，又让 s.Budget.Stats(adv.ID) 断言仍然成立。
	snap.Campaigns[adv.ID] = &config.Campaign{
		ID: adv.ID, AdvertiserID: adv.ID, Status: "active", DailyBudget: 1e9,
	}
	snap.CreativeCampaign["cr_t"] = adv.ID

	discard := slog.New(slog.DiscardHandler)
	cache, err := config.NewCache(context.Background(), &fixedLoader{snap}, discard)
	if err != nil {
		t.Fatal(err)
	}
	bud := budget.NewMemory(balances, nil)
	s := &Server{
		Cache: cache, Engine: &engine.Engine{Freq: benchFreqPassthrough{}, Budget: bud},
		Metrics: metrics.New(), Budget: bud, Log: discard,
	}
	s.Queue = queue.NewMemory(8192, 500, 2*time.Second) // 不启动消费协程：单测只验证入队
	return s
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func postEvent(t *testing.T, s *Server, apiKey, body string) int {
	t.Helper()
	r := httptest.NewRequest("POST", "/v1/ad/event", strings.NewReader(body))
	r.Header.Set("X-Api-Key", apiKey)
	w := httptest.NewRecorder()
	s.handleAdEvent(w, r)
	return w.Code
}

const testAPIKey = "adc_test_0000000000000000000000000001"

func TestEvent_CPM_impression扣费_click不扣(t *testing.T) {
	adv := &config.Advertiser{ID: "a1", Status: "active", BillingMode: "cpm", BiddingPrice: 5.0} // CPM $5 → 每次曝光 $0.005
	s := billTestServer(t, adv, map[string][2]float64{"a1": {100, 0}})

	if code := postEvent(t, s, testAPIKey,
		`{"event":"impression","style":"rewarded_video","deviceId":"d1","advertiserId":"a1","creativeId":"cr_t"}`); code != 200 {
		t.Fatalf("impression 应 200，实际 %d", code)
	}
	spent, _ := s.Budget.Stats("a1")
	if !near(spent, 0.005) {
		t.Fatalf("cpm impression 应扣 5/1000=0.005，实际 %v", spent)
	}
	// click 不是 cpm 的计费事件 → 不扣
	if code := postEvent(t, s, testAPIKey,
		`{"event":"click","style":"rewarded_video","deviceId":"d1","advertiserId":"a1","creativeId":"cr_t"}`); code != 200 {
		t.Fatalf("click 应 200，实际 %d", code)
	}
	if spent, _ := s.Budget.Stats("a1"); !near(spent, 0.005) {
		t.Fatalf("cpm 广告主 click 不应扣费，实际 %v", spent)
	}
}

func TestEvent_CPC_click扣费_impression不扣(t *testing.T) {
	adv := &config.Advertiser{ID: "a2", Status: "active", BillingMode: "cpc", BiddingPrice: 0.3}
	s := billTestServer(t, adv, map[string][2]float64{"a2": {100, 0}})

	if code := postEvent(t, s, testAPIKey,
		`{"event":"impression","style":"rewarded_video","deviceId":"d1","advertiserId":"a2","creativeId":"cr_t"}`); code != 200 {
		t.Fatal("impression 应 200")
	}
	if spent, _ := s.Budget.Stats("a2"); !near(spent, 0) {
		t.Fatalf("cpc 广告主 impression 不应扣费，实际 %v", spent)
	}
	if code := postEvent(t, s, testAPIKey,
		`{"event":"click","style":"rewarded_video","deviceId":"d1","advertiserId":"a2","creativeId":"cr_t"}`); code != 200 {
		t.Fatal("click 应 200")
	}
	if spent, _ := s.Budget.Stats("a2"); !near(spent, 0.3) {
		t.Fatalf("cpc click 应扣 0.3，实际 %v", spent)
	}
}

func TestEvent_CPA广告主客户端事件不扣费(t *testing.T) {
	adv := &config.Advertiser{ID: "a3", Status: "active", BillingMode: "cpa",
		BiddingPrice: 1.0, CPAEventPrices: map[string][2]float64{"install": {1.0, 1.0}}}
	s := billTestServer(t, adv, map[string][2]float64{"a3": {100, 0}})

	for _, evt := range []string{"impression", "click"} {
		body := fmt.Sprintf(`{"event":%q,"style":"rewarded_video","deviceId":"d1","advertiserId":"a3","creativeId":"cr_t"}`, evt)
		if code := postEvent(t, s, testAPIKey, body); code != 200 {
			t.Fatalf("%s 应 200，实际 %d", evt, code)
		}
	}
	if spent, _ := s.Budget.Stats("a3"); !near(spent, 0) {
		t.Fatalf("cpa 广告主客户端事件不应扣费（等 S2S 转化），实际 %v", spent)
	}
}

func TestEvent_客户端conversion被拒(t *testing.T) {
	adv := &config.Advertiser{ID: "a4", Status: "active", BillingMode: "cpa",
		BiddingPrice: 1.0, CPAEventPrices: map[string][2]float64{"install": {1.0, 1.0}}}
	s := billTestServer(t, adv, map[string][2]float64{"a4": {100, 0}})

	code := postEvent(t, s, testAPIKey,
		`{"event":"conversion","style":"rewarded_video","deviceId":"d1","advertiserId":"a4"}`)
	if code != 400 {
		t.Fatalf("客户端上报 conversion 应 400（走 S2S），实际 %d", code)
	}
	if spent, _ := s.Budget.Stats("a4"); !near(spent, 0) {
		t.Fatalf("被拒的 conversion 不应扣费，实际 %v", spent)
	}
}

func TestEvent_余额不足_事件照记不扣费(t *testing.T) {
	adv := &config.Advertiser{ID: "a5", Status: "active", BillingMode: "cpm", BiddingPrice: 1000.0} // 单次曝光 $1
	s := billTestServer(t, adv, map[string][2]float64{"a5": {10, 10}})                              // 已花完

	if code := postEvent(t, s, testAPIKey,
		`{"event":"impression","style":"rewarded_video","deviceId":"d1","advertiserId":"a5","creativeId":"cr_t"}`); code != 200 {
		t.Fatal("预算耗尽后真实曝光事件仍应 200（已发生，只是不扣）")
	}
	if spent, _ := s.Budget.Stats("a5"); !near(spent, 10) {
		t.Fatalf("余额不足不应扣费，spent 保持 10，实际 %v", spent)
	}
}

func s2sGet(t *testing.T, s *Server, query string) int {
	t.Helper()
	r := httptest.NewRequest("GET", "/v1/s2s/event?"+query, nil)
	w := httptest.NewRecorder()
	s.handleS2SEvent(w, r)
	return w.Code
}

// fakeClickResolver 固定 clickid → 点击上下文（模拟将来点击登记表）。
type fakeClickResolver struct{ m map[string]*ClickContext }

func (f fakeClickResolver) Resolve(clickID string) (*ClickContext, bool) {
	c, ok := f.m[clickID]
	return c, ok
}

func s2sResolvedServer(t *testing.T, adv *config.Advertiser, balances map[string][2]float64,
	clicks map[string]*ClickContext) *Server {
	s := billTestServer(t, adv, balances)
	if len(clicks) > 0 {
		s.Clicks = fakeClickResolver{m: clicks}
	}
	return s
}

func TestS2S_clickid归因_install扣费_无单价事件不扣(t *testing.T) {
	adv := &config.Advertiser{ID: "b1", Status: "active", BillingMode: "cpa",
		BiddingPrice: 1.0, CPAEventPrices: map[string][2]float64{"install": {1.0, 1.0}, "activate": {0.5, 0.5}}}
	ctx := &ClickContext{AppID: "app_t", Style: "rewarded_video", AdvertiserID: "b1",
		DeviceID: "u1", CreativeID: "cr_t"}
	s := s2sResolvedServer(t, adv, map[string][2]float64{"b1": {100, 0}},
		map[string]*ClickContext{"c1": ctx})

	base := "clickid=c1&event_name="
	if code := s2sGet(t, s, base+"install&pixelId=px1"); code != 200 {
		t.Fatalf("install 回调应 200，实际 %d", code)
	}
	if spent, _ := s.Budget.Stats("b1"); !near(spent, 1.0) {
		t.Fatalf("cpa install 应扣 1.0，实际 %v", spent)
	}
	// 有单价的事件才扣
	if code := s2sGet(t, s, base+"activate"); code != 200 {
		t.Fatalf("activate 回调应 200，实际 %d", code)
	}
	if spent, _ := s.Budget.Stats("b1"); !near(spent, 1.5) {
		t.Fatalf("activate 应扣 0.5（累计 1.5），实际 %v", spent)
	}
	// 未配置单价的事件（register）→ 记录不扣
	if code := s2sGet(t, s, base+"register"); code != 200 {
		t.Fatalf("register 回调应 200，实际 %d", code)
	}
	if spent, _ := s.Budget.Stats("b1"); !near(spent, 1.5) {
		t.Fatalf("未配置单价事件不应扣费，实际 %v", spent)
	}
}

func TestS2S_purchase带currency_value(t *testing.T) {
	adv := &config.Advertiser{ID: "b2", Status: "active", BillingMode: "cpa",
		BiddingPrice: 1.0, CPAEventPrices: map[string][2]float64{"install": {1.0, 1.0}, "purchase": {0.2, 0.2}}}
	ctx := &ClickContext{AppID: "app_t", Style: "rewarded_video", AdvertiserID: "b2", DeviceID: "u1", CreativeID: "cr_t"}
	s := s2sResolvedServer(t, adv, map[string][2]float64{"b2": {100, 0}},
		map[string]*ClickContext{"c2": ctx})

	// 充值事件：currency + value 回传；计费仍按 cpa_event_prices 固定单价，value 只记录
	if code := s2sGet(t, s,
		"clickid=c2&event_name=purchase&pixelId=px2&currency=USD&value=49.9"); code != 200 {
		t.Fatal("purchase 回调应 200")
	}
	if spent, _ := s.Budget.Stats("b2"); !near(spent, 0.2) {
		t.Fatalf("purchase 应按单价 0.2 扣费（value 49.9 只记录不参与），实际 %v", spent)
	}
	// 非充值事件可不带 currency/value
	if code := s2sGet(t, s, "clickid=c2&event_name=install"); code != 200 {
		t.Fatal("install 无 currency/value 应 200")
	}
	if spent, _ := s.Budget.Stats("b2"); !near(spent, 1.2) {
		t.Fatalf("install 应再扣 1.0（累计 1.2），实际 %v", spent)
	}
}

func TestS2S_未接入clickid登记_只确认不扣费(t *testing.T) {
	adv := &config.Advertiser{ID: "b3", Status: "active", BillingMode: "cpa",
		BiddingPrice: 1.0, CPAEventPrices: map[string][2]float64{"install": {1.0, 1.0}}}
	s := s2sResolvedServer(t, adv, map[string][2]float64{"b3": {100, 0}}, nil) // 未注入 resolver

	if code := s2sGet(t, s, "clickid=unknown_c&event_name=install"); code != 200 {
		t.Fatal("归因不到的转化应 200 确认（防归因方重试放大）")
	}
	if spent, _ := s.Budget.Stats("b3"); !near(spent, 0) {
		t.Fatalf("未归因转化不应扣费，实际 %v", spent)
	}
	// 已登记 clickid 但无 resolver 同理
	if code := s2sGet(t, s, "clickid=whatever&event_name=install"); code != 200 {
		t.Fatal("应 200")
	}
}

func TestS2S_testFlag_确认不记账(t *testing.T) {
	adv := &config.Advertiser{ID: "b4", Status: "active", BillingMode: "cpa",
		BiddingPrice: 1.0, CPAEventPrices: map[string][2]float64{"install": {1.0, 1.0}}}
	ctx := &ClickContext{AppID: "app_t", Style: "rewarded_video", AdvertiserID: "b4", DeviceID: "u1"}
	s := s2sResolvedServer(t, adv, map[string][2]float64{"b4": {100, 0}},
		map[string]*ClickContext{"c4": ctx})

	if code := s2sGet(t, s, "clickid=c4&event_name=install&testFlag=true"); code != 200 {
		t.Fatal("测试流量应 200")
	}
	if spent, _ := s.Budget.Stats("b4"); !near(spent, 0) {
		t.Fatalf("testFlag 流量不应扣费，实际 %v", spent)
	}
}

func TestS2S_非CPA广告主转化不扣费(t *testing.T) {
	adv := &config.Advertiser{ID: "b5", Status: "active", BillingMode: "cpm", BiddingPrice: 5.0}
	ctx := &ClickContext{AppID: "app_t", Style: "rewarded_video", AdvertiserID: "b5", DeviceID: "u1"}
	s := s2sResolvedServer(t, adv, map[string][2]float64{"b5": {100, 0}},
		map[string]*ClickContext{"c5": ctx})

	if code := s2sGet(t, s, "clickid=c5&event_name=install"); code != 200 {
		t.Fatal("cpm 广告主收到 install 回调应 200（确认收到）")
	}
	if spent, _ := s.Budget.Stats("b5"); !near(spent, 0) {
		t.Fatalf("cpm 广告主的 install 回调不应扣费，实际 %v", spent)
	}
}

func TestS2S_参数校验(t *testing.T) {
	adv := &config.Advertiser{ID: "b6", Status: "active", BillingMode: "cpa",
		BiddingPrice: 1.0, CPAEventPrices: map[string][2]float64{"install": {1.0, 1.0}}}
	s := s2sResolvedServer(t, adv, map[string][2]float64{"b6": {100, 0}},
		map[string]*ClickContext{"c6": {AppID: "app_t", Style: "rewarded_video", AdvertiserID: "b6"}})

	ok := "clickid=c6&event_name=install&pixelId=px6"
	if code := s2sGet(t, s, ok); code != 200 {
		t.Fatal("合法回调应 200")
	}
	cases := map[string]string{
		"缺clickid":   "event_name=install",
		"缺event":     "clickid=c6",
		"非法event":   "clickid=c6&event_name=hack",
		"value非数字": "clickid=c6&event_name=install&value=abc",
	}
	for name, q := range cases {
		if code := s2sGet(t, s, q); code == 200 {
			t.Fatalf("%s 应被拒绝，实际 200", name)
		}
	}
}
