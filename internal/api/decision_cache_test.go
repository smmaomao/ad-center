package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"adcenter/internal/cache"
	"adcenter/internal/config"
	"adcenter/internal/engine"
	"adcenter/internal/metrics"
)

// ---- 测试替身 ----

type fakeLoader struct{ snap *config.Snapshot }

func (f *fakeLoader) LoadSnapshot(context.Context) (*config.Snapshot, error) {
	return f.snap, nil
}

// countingDecider 统计 Decide 调用次数（验证缓存是否生效）。
type countingDecider struct {
	calls int
	resp  engine.Response
}

func (d *countingDecider) Decide(_ *config.Snapshot, _ engine.Request) engine.Response {
	d.calls++
	return d.resp
}

func (d *countingDecider) BudgetSnapshot(_ *config.Snapshot) map[string][2]float64 {
	return nil
}

func (d *countingDecider) WalletBalances(_ *config.Snapshot) map[string]float64 {
	return nil
}

func (d *countingDecider) DecideWith(_ *config.Snapshot, _ engine.Request, _ map[string][2]float64, _ map[string]float64) engine.Response {
	d.calls++
	return d.resp
}

func (d *countingDecider) DecideStyles(_ *config.Snapshot, _ engine.Request, _ map[string][2]float64, _ map[string]float64) []engine.Item {
	d.calls++
	return d.resp.Items
}

// fakeCache 内存实现 cache.DecisionCache，记录 Set 次数。
type fakeCache struct {
	m    map[string][]byte
	sets int
}

func (f *fakeCache) Get(_ context.Context, key string) ([]byte, bool, error) {
	v, ok := f.m[key]
	return v, ok, nil
}
func (f *fakeCache) Set(_ context.Context, key string, val []byte, _ time.Duration) error {
	if f.m == nil {
		f.m = map[string][]byte{}
	}
	f.m[key] = val
	f.sets++
	return nil
}

func testSnapshot() *config.Snapshot {
	key := "test-key"
	sum := sha256.Sum256([]byte(key))
	hash := hex.EncodeToString(sum[:])
	app := &config.App{ID: "app1", Status: "active"}
	return &config.Snapshot{
		Apps:                  map[string]*config.App{"app1": app},
		AppByKeyHash:          map[string]*config.App{hash: app},
		Advertisers:           map[string]*config.Advertiser{"adv1": {ID: "adv1", Name: "A"}},
		Campaigns:             map[string]*config.Campaign{"cmp1": {ID: "cmp1", Status: "active", DeliverTTLMinutes: 7, CreativeIDs: []string{"cr1"}}},
		CreativeCampaign:      map[string]string{"cr1": "cmp1"},
		CreativesByAdvertiser: map[string][]*config.Creative{},
		CreativesByID:         map[string]*config.Creative{"cr1": {ID: "cr1", Status: "active", Styles: []string{"rewarded_video"}}},
		Settings:              map[string]json.RawMessage{},
	}
}

func newTestServer(t *testing.T, snap *config.Snapshot, dec Decider, dc cache.DecisionCache) *Server {
	t.Helper()
	c, err := config.NewCache(context.Background(), &fakeLoader{snap: snap},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("new cache: %v", err)
	}
	return &Server{Cache: c, Engine: dec, Metrics: metrics.New(), DecisionCache: dc,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

// doAdReq 直接驱动 resolveDecision（旧 /v1/ad/req handler 已废弃，决策缓存逻辑不变）：
// 第一次 miss → 引擎计算 + 写入缓存；第二次 hit → 命中缓存，引擎不再计算。
func doAdReq(t *testing.T, srv *Server) string {
	t.Helper()
	snap := srv.Cache.Snapshot()
	app := &config.App{ID: "app_t", Status: "active"}
	req := adRequest{Style: "rewarded_video", DeviceID: "d1", Count: 1}
	resp := srv.resolveDecision(httptest.NewRequest(http.MethodPost, "/x", nil), snap, app, req, time.Now())
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestDecisionCache_Hit(t *testing.T) {
	snap := testSnapshot()
	// 决策缓存兜底已改为默认关闭（见 DefaultDecisionCacheConfig 注释：生产路径
	// 从不调用它，避免误开后热路径多出 GET+SET）。本用例验证缓存逻辑本身，故显式开启。
	if snap.Settings == nil {
		snap.Settings = map[string]json.RawMessage{}
	}
	snap.Settings["decision_cache"] = json.RawMessage(`{"enabled":true,"ttl_seconds":300}`)
	dec := &countingDecider{resp: engine.Response{Items: []engine.Item{{AdvertiserID: "adv1", Advertiser: "A", Creative: &config.Creative{ID: "cr1"}}}}}
	fc := &fakeCache{}
	srv := newTestServer(t, snap, dec, fc)

	doAdReq(t, srv)           // miss → 计算 + 写入
	second := doAdReq(t, srv) // hit → 命中缓存

	if dec.calls != 1 {
		t.Fatalf("引擎应仅计算一次（缓存命中），实际 %d", dec.calls)
	}
	if fc.sets != 1 {
		t.Fatalf("缓存应写入一次，实际 %d", fc.sets)
	}

	// 命中后 expire_at 应按当前时间与 adv1 的 7 分钟配置重算（>0）
	var out engine.Response
	if err := json.Unmarshal([]byte(second), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 1 || out.Items[0].ExpireAt == 0 {
		t.Fatalf("expire_at 命中后应被重算，got %+v", out.Items)
	}
}

func TestDecisionCache_NoCacheCallsEngineEachTime(t *testing.T) {
	snap := testSnapshot()
	dec := &countingDecider{resp: engine.Response{Items: []engine.Item{{AdvertiserID: "adv1", Creative: &config.Creative{ID: "cr1"}}}}}
	srv := newTestServer(t, snap, dec, nil) // 无缓存

	doAdReq(t, srv)
	doAdReq(t, srv)
	if dec.calls != 2 {
		t.Fatalf("无缓存时每次都应计算，实际 %d", dec.calls)
	}
}

func TestDecisionCacheKey(t *testing.T) {
	a := adRequest{Style: "rewarded_video", DeviceID: "d", Country: "US", Language: "en", Count: 1}
	b := a
	b.DeviceID = "other"
	if decisionCacheKey("app", "rewarded_video", a) == decisionCacheKey("app", "rewarded_video", b) {
		t.Fatal("不同 device 应不同键")
	}
	z := a
	z.Count = 0 // 归一为 1
	if decisionCacheKey("app", "rewarded_video", a) != decisionCacheKey("app", "rewarded_video", z) {
		t.Fatal("count=0 应归一为 1，与 count=1 同键")
	}
	big := a
	big.Count = engine.MaxCount + 100 // 归一为 MaxCount
	if decisionCacheKey("app", "rewarded_video", a) == decisionCacheKey("app", "rewarded_video", big) {
		t.Fatal("超大 count 应归一，与 1 不同键")
	}
}

func TestDecisionCacheConfig(t *testing.T) {
	// 无 Settings → 默认 disabled/300（生产路径从不调用决策缓存，默认关闭避免
	// 误开后热路径多出 GET+SET；见 DefaultDecisionCacheConfig 注释）
	if c := (&config.Snapshot{}).DecisionCacheConfig(); c.Enabled || c.TTLSeconds != 300 {
		t.Fatalf("默认应 disabled/300, got %+v", c)
	}
	// 缺失 key → 兜底默认
	if c := (&config.Snapshot{Settings: map[string]json.RawMessage{}}).DecisionCacheConfig(); c.Enabled || c.TTLSeconds != 300 {
		t.Fatalf("缺失 key 应兜底 disabled/300, got %+v", c)
	}
	// 非法 JSON → 兜底默认
	if c := (&config.Snapshot{Settings: map[string]json.RawMessage{"decision_cache": json.RawMessage(`not-json`)}}).DecisionCacheConfig(); c.Enabled {
		t.Fatalf("非法 JSON 应兜底默认 disabled")
	}
	// ttl<=0 → 兜底 300 但保留 enabled 配置
	if c := (&config.Snapshot{Settings: map[string]json.RawMessage{"decision_cache": json.RawMessage(`{"enabled":false,"ttl_seconds":0}`)}}).DecisionCacheConfig(); c.Enabled || c.TTLSeconds != 300 {
		t.Fatalf("ttl=0 应兜底 300, got %+v", c)
	}
	// 有效配置
	if c := (&config.Snapshot{Settings: map[string]json.RawMessage{"decision_cache": json.RawMessage(`{"enabled":true,"ttl_seconds":600}`)}}).DecisionCacheConfig(); !c.Enabled || c.TTLSeconds != 600 {
		t.Fatalf("应 600, got %+v", c)
	}
}
