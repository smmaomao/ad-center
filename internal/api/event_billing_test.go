package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"math"
	"net/http/httptest"
	"testing"
	"time"

	"adcenter/internal/budget"
	"adcenter/internal/config"
	"adcenter/internal/engine"
	"adcenter/internal/metrics"
	"adcenter/internal/queue"
)

// 计费模型集成测试：扣费只发生在真实计费事件，金额与锚点由 **campaign** 决定
// （campaign 是计费执行粒度；广告主只做钱包）：
//   cpm → impression（BiddingPrice/1000）；cpc → click；cpi/cpa-* → S2S 转化事件。
// 客户端通道不再接受 conversion；金额由服务端计算，不信上报。

// billTestServer 构造计费测试服务：camp 为计费/预算执行单元，其 ID 取 campID，
// 使 s.Budget.Stats(campID) 与请求体里的 advertiserId 断言一致。
func billTestServer(t *testing.T, campID string, camp *config.Campaign, balances map[string][2]float64) *Server {
	t.Helper()
	const apiKey = "adc_test_0000000000000000000000000001"
	sum := sha256.Sum256([]byte(apiKey))
	keyHash := hex.EncodeToString(sum[:])

	snap := &config.Snapshot{
		Apps:                  map[string]*config.App{},
		AppByKeyHash:          map[string]*config.App{},
		Advertisers:           map[string]*config.Advertiser{},
		CreativesByAdvertiser: map[string][]*config.Creative{},
		CreativesByID:         map[string]*config.Creative{},
		Campaigns:             map[string]*config.Campaign{},
		CreativeCampaign:      map[string]string{},
	}
	app := &config.App{ID: "app_t", Status: "active"}
	snap.Apps[app.ID] = app
	snap.AppByKeyHash[keyHash] = app
	// 广告主只承载身份（计费配置在 campaign）；出价/计费方式/CPA 单价由 camp 提供。
	snap.Advertisers[campID] = &config.Advertiser{ID: campID, Status: "active"}
	snap.CreativesByAdvertiser[campID] = []*config.Creative{{
		ID: "cr_t", AdvertiserID: campID, MediaType: "video", Status: "active",
	}}
	snap.CreativesByID["cr_t"] = snap.CreativesByAdvertiser[campID][0]
	camp.ID = campID
	camp.AdvertiserID = campID
	if camp.Status == "" {
		camp.Status = "active"
	}
	if camp.DailyBudget == 0 {
		camp.DailyBudget = 1e9
	}
	snap.Campaigns[campID] = camp
	snap.CreativeCampaign["cr_t"] = campID

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

func s2sResolvedServer(t *testing.T, campID string, camp *config.Campaign, balances map[string][2]float64,
	clicks map[string]*ClickContext) *Server {
	s := billTestServer(t, campID, camp, balances)
	if len(clicks) > 0 {
		s.Clicks = fakeClickResolver{m: clicks}
	}
	return s
}

func TestS2S_clickid归因_install扣费_非本事件不扣(t *testing.T) {
	camp := &config.Campaign{BillingMode: "cpi", BiddingPrice: 1.0}
	ctx := &ClickContext{AppID: "app_t", Style: "rewarded_video", AdvertiserID: "b1",
		DeviceID: "u1", CreativeID: "cr_t"}
	s := s2sResolvedServer(t, "b1", camp, map[string][2]float64{"b1": {100, 0}},
		map[string]*ClickContext{"c1": ctx})

	base := "clickid=c1&event_name="
	if code := s2sGet(t, s, base+"install&pixelId=px1"); code != 200 {
		t.Fatalf("install 回调应 200，实际 %d", code)
	}
	if spent, _ := s.Budget.Stats("b1"); !near(spent, 1.0) {
		t.Fatalf("cpi install 应扣 1.0，实际 %v", spent)
	}
	// 非本计费方式事件（activate/register）→ 记录不扣
	if code := s2sGet(t, s, base+"activate"); code != 200 {
		t.Fatalf("activate 回调应 200，实际 %d", code)
	}
	if spent, _ := s.Budget.Stats("b1"); !near(spent, 1.0) {
		t.Fatalf("非本计费事件不应扣费，实际 %v", spent)
	}
	if code := s2sGet(t, s, base+"register"); code != 200 {
		t.Fatalf("register 回调应 200，实际 %d", code)
	}
	if spent, _ := s.Budget.Stats("b1"); !near(spent, 1.0) {
		t.Fatalf("非本计费事件不应扣费，实际 %v", spent)
	}
}

func TestS2S_purchase带currency_value(t *testing.T) {
	camp := &config.Campaign{BillingMode: "cpa-pay", BiddingPrice: 0.2}
	ctx := &ClickContext{AppID: "app_t", Style: "rewarded_video", AdvertiserID: "b2", DeviceID: "u1", CreativeID: "cr_t"}
	s := s2sResolvedServer(t, "b2", camp, map[string][2]float64{"b2": {100, 0}},
		map[string]*ClickContext{"c2": ctx})

	// 充值事件：currency + value 回传；计费按本计费方式单价，value 只记录
	if code := s2sGet(t, s,
		"clickid=c2&event_name=purchase&pixelId=px2&currency=USD&value=49.9"); code != 200 {
		t.Fatal("purchase 回调应 200")
	}
	if spent, _ := s.Budget.Stats("b2"); !near(spent, 0.2) {
		t.Fatalf("purchase 应按单价 0.2 扣费（value 49.9 只记录不参与），实际 %v", spent)
	}
	// 非本计费事件（install）不扣
	if code := s2sGet(t, s, "clickid=c2&event_name=install"); code != 200 {
		t.Fatal("install 无 currency/value 应 200")
	}
	if spent, _ := s.Budget.Stats("b2"); !near(spent, 0.2) {
		t.Fatalf("非本计费事件不应扣费，实际 %v", spent)
	}
}

func TestS2S_未接入clickid登记_只确认不扣费(t *testing.T) {
	camp := &config.Campaign{BillingMode: "cpa-pay", BiddingPrice: 1.0}
	s := s2sResolvedServer(t, "b3", camp, map[string][2]float64{"b3": {100, 0}}, nil) // 未注入 resolver

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
	camp := &config.Campaign{BillingMode: "cpa-pay", BiddingPrice: 1.0}
	ctx := &ClickContext{AppID: "app_t", Style: "rewarded_video", AdvertiserID: "b4", DeviceID: "u1"}
	s := s2sResolvedServer(t, "b4", camp, map[string][2]float64{"b4": {100, 0}},
		map[string]*ClickContext{"c4": ctx})

	if code := s2sGet(t, s, "clickid=c4&event_name=install&testFlag=true"); code != 200 {
		t.Fatal("测试流量应 200")
	}
	if spent, _ := s.Budget.Stats("b4"); !near(spent, 0) {
		t.Fatalf("testFlag 流量不应扣费，实际 %v", spent)
	}
}

func TestS2S_非CPA任务转化不扣费(t *testing.T) {
	camp := &config.Campaign{BillingMode: "cpm", BiddingPrice: 5.0}
	ctx := &ClickContext{AppID: "app_t", Style: "rewarded_video", AdvertiserID: "b5", DeviceID: "u1"}
	s := s2sResolvedServer(t, "b5", camp, map[string][2]float64{"b5": {100, 0}},
		map[string]*ClickContext{"c5": ctx})

	if code := s2sGet(t, s, "clickid=c5&event_name=install"); code != 200 {
		t.Fatal("cpm 任务收到 install 回调应 200（确认收到）")
	}
	if spent, _ := s.Budget.Stats("b5"); !near(spent, 0) {
		t.Fatalf("cpm 任务的 install 回调不应扣费，实际 %v", spent)
	}
}

func TestS2S_参数校验(t *testing.T) {
	camp := &config.Campaign{BillingMode: "cpa-pay", BiddingPrice: 1.0}
	s := s2sResolvedServer(t, "b6", camp, map[string][2]float64{"b6": {100, 0}},
		map[string]*ClickContext{"c6": {AppID: "app_t", Style: "rewarded_video", AdvertiserID: "b6"}})

	ok := "clickid=c6&event_name=install&pixelId=px6"
	if code := s2sGet(t, s, ok); code != 200 {
		t.Fatal("合法回调应 200")
	}
	cases := map[string]string{
		"缺clickid": "event_name=install",
		"缺event":   "clickid=c6",
		"非法event":  "clickid=c6&event_name=hack",
		"value非数字": "clickid=c6&event_name=install&value=abc",
	}
	for name, q := range cases {
		if code := s2sGet(t, s, q); code == 200 {
			t.Fatalf("%s 应被拒绝，实际 200", name)
		}
	}
}
