package engine

import (
	"math"
	"testing"
	"time"

	"adcenter/internal/budget"
	"adcenter/internal/config"
	"adcenter/internal/frequency"
)

// ===== 测试替身 =====

type fakeFreq struct {
	denyAdv map[string]bool // 拒绝这些 campaign（Check）
	denyAll bool
}

func (f *fakeFreq) CheckSlot(_ string, _ string, _ string, _ frequency.SlotPolicy, _ int, _ time.Time) bool {
	return true
}
func (f *fakeFreq) RecordSlot(string, string, string, int, time.Time) {}
func (f *fakeFreq) CheckAndIncr(_, _, _, advID string, _ frequency.AdvPolicy, _ time.Time) bool {
	if f.denyAll {
		return false
	}
	return !f.denyAdv[advID]
}
func (f *fakeFreq) Check(_, _, _, advID string, _ frequency.AdvPolicy, _ time.Time) bool {
	if f.denyAll {
		return false
	}
	return !f.denyAdv[advID]
}
func (f *fakeFreq) Record(string, string, string, string, frequency.AdvPolicy, time.Time) {}

type fakeBudget struct {
	spent  map[string]float64
	budget map[string]float64
	deny   map[string]bool
	wallet map[string]float64 // 已启用钱包余额；缺失 = 不限制
	deduct []string           // TryDeduct 成功记录（事件扣费路径；决策路径不应触发）
}

// fakeBudget 预算控制替身。
func (b *fakeBudget) TryDeduct(advID string, amount float64) bool {
	if b.deny[advID] {
		return false
	}
	b.spent[advID] += amount
	b.deduct = append(b.deduct, advID)
	return true
}
func (b *fakeBudget) Stats(advID string) (float64, float64) {
	return b.spent[advID], b.budget[advID]
}
func (b *fakeBudget) HourlyCalibrate() error { return nil }

// 广告主总钱包替身：wallet 为 nil 或未收录该广告主 → 不限制（MaxFloat64 / 放行），
// 保证既有预算用例不受总余额闸影响。
func (b *fakeBudget) WalletBalance(advID string) float64 {
	if v, ok := b.wallet[advID]; ok {
		return v
	}
	return math.MaxFloat64
}
func (b *fakeBudget) WalletDeduct(advID string, amount float64) bool {
	v, ok := b.wallet[advID]
	if !ok {
		return true
	}
	if v < amount {
		return false
	}
	b.wallet[advID] = v - amount
	return true
}
func (b *fakeBudget) WalletCredit(advID string, amount float64) {
	if b.wallet == nil {
		b.wallet = map[string]float64{}
	}
	b.wallet[advID] += amount
}

var _ budget.Ctrl = (*fakeBudget)(nil)

// ===== 测试工具 =====

// testNow Jakarta 10:00（进度 10/24≈0.417），用于消耗节奏用例。
var testNow = time.Date(2026, 9, 4, 3, 0, 0, 0, time.UTC)

// advSpec 测试用：一个广告主 + 其默认广告任务的 KPI/预算配置。
// KPI（target/actual CPI）现属 campaign（与线上一致），由 mkAdv 落到 camp 上。
type advSpec struct {
	adv  *config.Advertiser
	camp *config.Campaign
}

// mkSnapshot 构造测试快照：每个广告主挂一个默认素材与一个对应 campaign
// （预算/KPI 执行粒度），creative 归属 campaign 以验证「按 campaign 封顶预算」逻辑。
func mkSnapshot(specs ...advSpec) *config.Snapshot {
	snap := &config.Snapshot{
		Apps:                  map[string]*config.App{"app1": {ID: "app1", Status: "active"}},
		Advertisers:           map[string]*config.Advertiser{},
		CreativesByAdvertiser: map[string][]*config.Creative{},
		CreativesByID:         map[string]*config.Creative{},
		Campaigns:             map[string]*config.Campaign{},
		CreativeCampaign:      map[string]string{},
		PricingBenchmark:      config.DefaultPricingBenchmark(),
	}
	for _, s := range specs {
		a := s.adv
		snap.Advertisers[a.ID] = a
		cr := &config.Creative{
			ID: "cr_" + a.ID, AdvertiserID: a.ID, Status: "active",
			MediaType: "video",
			Styles:    []string{"rewarded_video"},
		}
		snap.CreativesByAdvertiser[a.ID] = []*config.Creative{cr}
		snap.CreativesByID[cr.ID] = cr
		// 每个广告主对应一个 campaign（预算单元），其素材归属该 campaign；
		// campaign 携带 KPI（target/actual CPI），是 KPI 执行粒度。
		camp := s.camp
		camp.BillingMode = "cpm"
		camp.BiddingPrice = 15 // Price_Score = 15/15*100 = 100
		camp.CreativeIDs = []string{cr.ID}
		snap.Campaigns[camp.ID] = camp
		snap.CreativeCampaign[cr.ID] = camp.ID
	}
	return snap
}

// mkAdv 构造测试广告主 + 其默认 campaign。达成率方向 target/actual
// （actual 越大 = 成本越高 = 越紧急），KPI 现落在 campaign 上。
func mkAdv(id string, _ int, target, actual float64) advSpec {
	return advSpec{
		adv: &config.Advertiser{ID: id, Name: id, Status: "active"},
		camp: &config.Campaign{
			ID: "cmp_" + id, AdvertiserID: id, Status: "active",
			DailyBudget: 1000, TargetKPIValue: target,
		},
	}
}

func mkEngine(freq *fakeFreq, bud *fakeBudget) *Engine {
	return &Engine{Freq: freq, Budget: bud, Pick: func(n int) int { return 0 }}
}

func mkBudget() *fakeBudget {
	return &fakeBudget{spent: map[string]float64{}, budget: map[string]float64{}, deny: map[string]bool{}}
}

func ids(items []Item) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.AdvertiserID
	}
	return out
}

// ===== 用例 =====

// TestDecideSingle_KPI紧急度排序 已移除：实测 CPI 不再落库，达成率暂置中性 1.0，
// KPI 紧急度区分（urgent 应胜过 ok）待后期引入运行时实测值后再恢复。

func TestDecideSingle_无Tier层权重(t *testing.T) {
	// 彻底去掉 Tier 后，顺序完全由 KPI 达成率（紧急度）决定，不再有层级起跑权重。
	// 相同达成率（0.9）的广告主平局时按素材 ID 升序 → cr_t1 胜出。
	snap := mkSnapshot(
		mkAdv("t3", 3, 1.0, 0.9),
		mkAdv("t1", 1, 1.0, 0.9),
		mkAdv("t2", 2, 1.0, 0.9),
	)
	req := Request{App: snap.Apps["app1"], Style: "rewarded_video",
		DeviceID: "d1", Count: 1, Now: testNow}
	resp := mkEngine(&fakeFreq{}, mkBudget()).Decide(snap, req)
	if resp.Items[0].AdvertiserID != "t1" {
		t.Fatalf("无 Tier 后平局应按素材 ID 升序选 t1，实际 %v", ids(resp.Items))
	}
}

func TestDecide_消耗节奏系数(t *testing.T) {
	// Jakarta 10:00 预期进度 ≈ 0.417
	// slow 实际 0.1（落后 >10%）→ ×1.3；fast 实际 0.9（超前）→ ×0.8
	// 两者达成率/出价相同，slow 应胜出
	snap := mkSnapshot(
		mkAdv("slow", 1, 1.0, 0.9),
		mkAdv("fast", 1, 1.0, 0.9),
	)
	bud := mkBudget()
	bud.budget["cmp_slow"], bud.spent["cmp_slow"] = 100, 10
	bud.budget["cmp_fast"], bud.spent["cmp_fast"] = 100, 90
	req := Request{App: snap.Apps["app1"], Style: "rewarded_video",
		DeviceID: "d1", Count: 1, Now: testNow}
	resp := mkEngine(&fakeFreq{}, bud).Decide(snap, req)
	if resp.Items[0].AdvertiserID != "slow" {
		t.Fatalf("消耗偏慢者应加速胜出，实际 %v", ids(resp.Items))
	}
}

func TestDecideSingle_总余额耗尽停投(t *testing.T) {
	// 广告主已启用总钱包且余额为 0 → 即使 campaign 日预算充足也必须停投并降级。
	snap := mkSnapshot(mkAdv("adv_broke", 1, 1.0, 1.0))
	bud := mkBudget()
	bud.wallet = map[string]float64{"adv_broke": 0}
	req := Request{App: snap.Apps["app1"], Style: "rewarded_video",
		DeviceID: "d1", Count: 1, Now: testNow}
	resp := mkEngine(&fakeFreq{}, bud).Decide(snap, req)
	if len(resp.Items) != 0 {
		t.Fatalf("总余额耗尽应停投，实际下发 %v", ids(resp.Items))
	}
	if resp.Fallback == "" {
		t.Fatal("停投应返回降级 fallback")
	}
}

func TestDecideSingle_未启用钱包不受限(t *testing.T) {
	// 未收录进钱包表（未启用钱包的存量广告主）→ 不受总余额闸限制，照常下发。
	snap := mkSnapshot(mkAdv("adv_normal", 1, 1.0, 1.0))
	bud := mkBudget()
	bud.wallet = map[string]float64{"adv_other": 0} // 仅别的广告主启用
	req := Request{App: snap.Apps["app1"], Style: "rewarded_video",
		DeviceID: "d1", Count: 1, Now: testNow}
	resp := mkEngine(&fakeFreq{}, bud).Decide(snap, req)
	if len(resp.Items) != 1 || resp.Items[0].AdvertiserID != "adv_normal" {
		t.Fatalf("未启用钱包的广告主应照常下发，实际 %v", ids(resp.Items))
	}
}

func TestDecideSingle_疲劳过滤顺延(t *testing.T) {
	// 最高分 adv1（达成率 50%，紧急）被任务级疲劳频控拒绝 → 顺延 adv2
	snap := mkSnapshot(
		mkAdv("adv1", 1, 1.0, 2.0),
		mkAdv("adv2", 1, 1.0, 1.1),
	)
	// 任务级滑动窗口频控：窗口 20min 内最多 3 次（两个字段都 >0 才生效）
	snap.Campaigns["cmp_adv1"].FreqIntervalMinute = 20
	snap.Campaigns["cmp_adv1"].FreqFatigueWindow = 3
	freq := &fakeFreq{denyAdv: map[string]bool{"cmp_adv1": true}}
	req := Request{App: snap.Apps["app1"], Style: "rewarded_video",
		DeviceID: "d1", Count: 1, Now: testNow}
	resp := mkEngine(freq, mkBudget()).Decide(snap, req)
	if len(resp.Items) != 1 || resp.Items[0].AdvertiserID != "adv2" {
		t.Fatalf("应顺延到 adv2，实际 %v", ids(resp.Items))
	}
}

func TestDecideSingle_预算耗尽顺延(t *testing.T) {
	// 新计费模型：req 不扣费，预算只做只读闸。
	// adv1 分数最高（达成率 50%）但当日预算已花完（spent == budget）→ 不再下发，顺延 adv2
	snap := mkSnapshot(
		mkAdv("adv1", 1, 1.0, 2.0),
		mkAdv("adv2", 1, 1.0, 1.1),
	)
	bud := mkBudget()
	bud.budget["cmp_adv1"], bud.spent["cmp_adv1"] = 100, 100 // 已耗尽
	bud.budget["cmp_adv2"], bud.spent["cmp_adv2"] = 100, 0
	req := Request{App: snap.Apps["app1"], Style: "rewarded_video",
		DeviceID: "d1", Count: 1, Now: testNow}
	resp := mkEngine(&fakeFreq{}, bud).Decide(snap, req)
	if len(resp.Items) != 1 || resp.Items[0].AdvertiserID != "adv2" {
		t.Fatalf("预算耗尽应顺延 adv2，实际 %v", ids(resp.Items))
	}
	if len(bud.deduct) != 0 {
		t.Fatalf("req 路径不应产生任何扣费（扣费在事件回执），实际 %v", bud.deduct)
	}
}

func TestDecide_定向过滤(t *testing.T) {
	adv := mkAdv("adv_id", 1, 1.0, 0.9)
	adv.adv.Targeting = config.Targeting{Countries: []string{"ID"}}
	snap := mkSnapshot(adv)
	req := Request{App: snap.Apps["app1"], Style: "rewarded_video",
		DeviceID: "d1", Country: "US", Count: 1, Now: testNow}
	resp := mkEngine(&fakeFreq{}, mkBudget()).Decide(snap, req)
	if resp.Fallback == "" {
		t.Fatal("定向不匹配（US vs 仅 ID）应降级")
	}
	// 命中定向则正常
	req.Country = "ID"
	resp = mkEngine(&fakeFreq{}, mkBudget()).Decide(snap, req)
	if len(resp.Items) != 1 {
		t.Fatal("命中定向应填充")
	}
}

func TestDecide_样式不匹配过滤(t *testing.T) {
	// 素材只支持 splash，请求 rewarded_video → 无候选降级
	snap := mkSnapshot(mkAdv("adv1", 1, 1.0, 0.9))
	snap.CreativesByAdvertiser["adv1"][0].Styles = []string{"splash"}
	req := Request{App: snap.Apps["app1"], Style: "rewarded_video",
		DeviceID: "d1", Count: 1, Now: testNow}
	resp := mkEngine(&fakeFreq{}, mkBudget()).Decide(snap, req)
	if resp.Fallback == "" {
		t.Fatal("样式不匹配应降级")
	}
	// 换成 splash 则命中
	req.Style = "splash"
	resp = mkEngine(&fakeFreq{}, mkBudget()).Decide(snap, req)
	if len(resp.Items) != 1 {
		t.Fatal("样式命中应填充")
	}
}

func TestDecide_投放App定向过滤(t *testing.T) {
	// 素材仅投放到 app2，请求来自 app1 → 无候选降级
	snap := mkSnapshot(mkAdv("adv1", 1, 1.0, 0.9))
	snap.CreativesByAdvertiser["adv1"][0].TargetApps = []string{"app2"}
	req := Request{App: snap.Apps["app1"], Style: "rewarded_video",
		DeviceID: "d1", Count: 1, Now: testNow}
	resp := mkEngine(&fakeFreq{}, mkBudget()).Decide(snap, req)
	if resp.Fallback == "" {
		t.Fatal("未投放到该 App 应降级")
	}
	// App 加入投放列表则命中
	snap.CreativesByAdvertiser["adv1"][0].TargetApps = []string{"app1"}
	resp = mkEngine(&fakeFreq{}, mkBudget()).Decide(snap, req)
	if len(resp.Items) != 1 {
		t.Fatal("投放到该 App 应填充")
	}
}

func TestDecide_投放截止过滤(t *testing.T) {
	end := testNow.Add(-time.Hour)
	adv := mkAdv("adv_end", 1, 1.0, 0.9)
	adv.camp.EndAt = &end
	snap := mkSnapshot(adv)
	req := Request{App: snap.Apps["app1"], Style: "rewarded_video",
		DeviceID: "d1", Count: 1, Now: testNow}
	resp := mkEngine(&fakeFreq{}, mkBudget()).Decide(snap, req)
	if resp.Fallback == "" {
		t.Fatal("已过投放截止时间应被过滤")
	}
}

func TestDecide_无可用降级(t *testing.T) {
	snap := mkSnapshot(mkAdv("adv1", 1, 1.0, 0.9))
	// 任务级滑动窗口频控开启后（窗口 20min/3 次），频控全拒 → 无候选 → 降级 self_promo
	snap.Campaigns["cmp_adv1"].FreqIntervalMinute = 20
	snap.Campaigns["cmp_adv1"].FreqFatigueWindow = 3
	req := Request{App: snap.Apps["app1"], Style: "rewarded_video",
		DeviceID: "d1", Count: 1, Now: testNow}
	// 频控全拒 → 无候选 → 降级 self_promo
	resp := mkEngine(&fakeFreq{denyAll: true}, mkBudget()).Decide(snap, req)
	if resp.Fallback != "self_promo" {
		t.Fatalf("无填充应降级 self_promo，实际 %q", resp.Fallback)
	}
}

func TestDecideBatch_topN与上限封顶(t *testing.T) {
	// 5 个候选，出价各异（其余评分因子相同 → 排序完全由 Price_Score 决定）。
	// cpm 标准线 15：Price 越大基准分越高。期望顺序 d(40)>b(30)>e(25)>a(20)>c(10)。
	snap := mkSnapshot(
		mkAdv("a", 1, 1.0, 1.0),
		mkAdv("b", 1, 1.0, 1.0),
		mkAdv("c", 1, 1.0, 1.0),
		mkAdv("d", 1, 1.0, 1.0),
		mkAdv("e", 1, 1.0, 1.0),
	)
	prices := map[string]float64{"a": 20, "b": 30, "c": 10, "d": 40, "e": 25}
	for id, p := range prices {
		snap.Campaigns["cmp_"+id].BiddingPrice = p
	}
	e := mkEngine(&fakeFreq{}, mkBudget())

	// count=3：取分数最高 3 个（d,b,e），按分数降序
	req := Request{App: snap.Apps["app1"], Style: "rewarded_video",
		DeviceID: "d1", Count: 3, Now: testNow}
	resp := e.Decide(snap, req)
	if len(resp.Items) != 3 {
		t.Fatalf("count=3 应返回 3 条，实际 %d", len(resp.Items))
	}
	want := []string{"d", "b", "e"}
	if got := ids(resp.Items); !slicesEqual(got, want) {
		t.Fatalf("count=3 名单/顺序错误：got %v want %v", got, want)
	}

	// count=10：候选仅 5 个 → 全部返回，顺序同上
	req.Count = 10
	resp = e.Decide(snap, req)
	if len(resp.Items) != 5 {
		t.Fatalf("count=10 候选仅 5 个应返回 5 条，实际 %d", len(resp.Items))
	}
	if got := ids(resp.Items); !slicesEqual(got, []string{"d", "b", "e", "a", "c"}) {
		t.Fatalf("count=10 顺序错误：got %v want %v", got, []string{"d", "b", "e", "a", "c"})
	}
}

func TestDecideBatch_物化失败不补位(t *testing.T) {
	snap := mkSnapshot(
		mkAdv("a", 1, 1.0, 2.0),
		mkAdv("b", 1, 1.0, 1.5),
	)
	// 任务级滑动窗口频控开启后（20min/3 次），按 campaign 拒绝 b（cmp_b）
	snap.Campaigns["cmp_b"].FreqIntervalMinute = 20
	snap.Campaigns["cmp_b"].FreqFatigueWindow = 3
	freq := &fakeFreq{denyAdv: map[string]bool{"cmp_b": true}}
	// count=4 但 b 被频控拒绝，只剩 a，最多 1 条
	req := Request{App: snap.Apps["app1"], Style: "rewarded_video",
		DeviceID: "d1", Count: 4, Now: testNow}
	resp := mkEngine(freq, mkBudget()).Decide(snap, req)
	if len(resp.Items) != 1 {
		t.Fatalf("b 被频控拒绝应只返回 a×1，实际 %v", ids(resp.Items))
	}
	for _, it := range resp.Items {
		if it.AdvertiserID != "a" {
			t.Fatalf("不应包含 b，实际 %v", ids(resp.Items))
		}
	}
}

func TestDecideBatch_count上限(t *testing.T) {
	// 25 个候选，count=999 应被钳制到 MaxCount=20
	specs := make([]advSpec, 25)
	for i := range specs {
		specs[i] = mkAdv(string(rune('a'+i)), 1, 1.0, 0.9)
	}
	snap := mkSnapshot(specs...)
	req := Request{App: snap.Apps["app1"], Style: "rewarded_video",
		DeviceID: "d1", Count: 999, Now: testNow}
	resp := mkEngine(&fakeFreq{}, mkBudget()).Decide(snap, req)
	if len(resp.Items) != MaxCount {
		t.Fatalf("count 应被钳制到 %d，实际 %d", MaxCount, len(resp.Items))
	}
}

func TestDecide_无活跃素材降级(t *testing.T) {
	snap := mkSnapshot(mkAdv("a", 1, 1.0, 0.9))
	snap.CreativesByAdvertiser["a"][0].Status = "paused"
	req := Request{App: snap.Apps["app1"], Style: "rewarded_video",
		DeviceID: "d1", Count: 1, Now: testNow}
	resp := mkEngine(&fakeFreq{}, mkBudget()).Decide(snap, req)
	if resp.Fallback == "" {
		t.Fatal("无活跃素材应降级")
	}
}

func TestDecide_CampaignFreq隐藏已达上限素材(t *testing.T) {
	// cmp_adv1 启用任务级频控且其窗口计数已达上限（决策期只读 Check 返回 false）
	// → 决策时隐藏并顺延 adv2。
	snap := mkSnapshot(
		mkAdv("adv1", 1, 1.0, 2.0),
		mkAdv("adv2", 1, 1.0, 1.0),
	)
	snap.Campaigns["cmp_adv1"].FreqIntervalMinute = 20
	snap.Campaigns["cmp_adv1"].FreqFatigueWindow = 3
	freq := &fakeFreq{denyAdv: map[string]bool{"cmp_adv1": true}}
	eng := mkEngine(freq, mkBudget())
	req := Request{App: snap.Apps["app1"], Style: "rewarded_video",
		DeviceID: "d1", Count: 1, Now: testNow}
	resp := eng.Decide(snap, req)
	if len(resp.Items) != 1 || resp.Items[0].AdvertiserID != "adv2" {
		t.Fatalf("频控素材应被隐藏并顺延 adv2，实际 %v", ids(resp.Items))
	}

	// 全部频控拒绝 → 降级 self_promo
	snap.Campaigns["cmp_adv2"].FreqIntervalMinute = 20
	snap.Campaigns["cmp_adv2"].FreqFatigueWindow = 3
	freq2 := &fakeFreq{denyAdv: map[string]bool{"cmp_adv1": true, "cmp_adv2": true}}
	eng2 := mkEngine(freq2, mkBudget())
	resp2 := eng2.Decide(snap, req)
	if resp2.Fallback != "self_promo" {
		t.Fatalf("全部频控拒绝应降级 self_promo，实际 %q items=%v", resp2.Fallback, ids(resp2.Items))
	}
}

func TestScore_消耗节奏边界(t *testing.T) {
	// 纯函数级：dayProgress 与 ±10% 阈值
	bud := &fakeBudget{spent: map[string]float64{}, budget: map[string]float64{}}
	adv := &config.Advertiser{ID: "x", Status: "active"}
	e := &Engine{Budget: bud}

	// Jakarta 10:00 → 实际 0.35（差 6.7%）→ 正常
	if got := e.pacingFactor(adv, testNow, 35, 100); got != 1.0 {
		t.Fatalf("偏差小于10%%时应为 1.0，实际 %v", got)
	}
	// 实际 0.30（差 11.7%）→ 偏慢加速
	if got := e.pacingFactor(adv, testNow, 30, 100); got != 1.3 {
		t.Fatalf("落后超过10%%应为 1.3，实际 %v", got)
	}
	// 实际 0.55（超前 13.3%）→ 偏快减速
	if got := e.pacingFactor(adv, testNow, 55, 100); got != 0.8 {
		t.Fatalf("超前超过10%%应为 0.8，实际 %v", got)
	}
}

func TestDecide_请求内可复现(t *testing.T) {
	// 同输入同输出（排序稳定性 + 确定性 tie-break）
	snap := mkSnapshot(
		mkAdv("x", 1, 1.0, 0.9),
		mkAdv("y", 1, 1.0, 0.9), // 与 x 完全同分
	)
	e := mkEngine(&fakeFreq{}, mkBudget())
	req := Request{App: snap.Apps["app1"], Style: "rewarded_video",
		DeviceID: "d1", Count: 1, Now: testNow}
	first := ids(e.Decide(snap, req).Items)
	for range 10 {
		got := ids(e.Decide(snap, req).Items)
		if got[0] != first[0] {
			t.Fatalf("同输入应同输出：首次 %v，本次 %v", first, got)
		}
	}
	// 平局按 creative.ID 升序 → cr_x 胜出
	if first[0] != "x" {
		t.Fatalf("平局应按 creative.ID 升序选 x，实际 %v", first)
	}
}

// ===== A1 / A2：KPI 达成率（实测 CPI 不再落库，暂置中性） =====

func TestAchievement_中性(t *testing.T) {
	// 实测 CPI 不再落库，Achievement 暂返回中性 1.0（见 Campaign.Achievement()）；
	// 后期自动优化引入运行时实测值后再恢复 target/实测 公式。
	cases := []struct {
		name string
		camp *config.Campaign
		want float64
	}{
		{"有目标", &config.Campaign{TargetKPIValue: 1.8}, 1.0},
		{"无目标", &config.Campaign{}, 1.0},
		{"零目标", &config.Campaign{TargetKPIValue: 0}, 1.0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.camp.Achievement(); got != tc.want {
				t.Fatalf("Achievement() = %v，期望 %v", got, tc.want)
			}
		})
	}
}

func TestDecide_冷启动不霸榜(t *testing.T) {
	// 实测 CPI 不再落库，达成率暂置中性 1.0（见 Campaign.Achievement()），
	// 因此冷启动广告主与"刚好达标"者同分档；KPI 紧急度区分待后期引入运行时实测值。
	bud := mkBudget()
	e := &Engine{Freq: &fakeFreq{}, Budget: bud}

	mkCr := func(adv *config.Advertiser) *config.Creative {
		return &config.Creative{ID: "cr", AdvertiserID: adv.ID, Status: "active",
			Styles: []string{"rewarded_video"}}
	}
	newC := e.scoreCreative(mkCr(&config.Advertiser{ID: "new"}),
		&config.Advertiser{ID: "new"},
		&config.Campaign{BillingMode: "cpm", BiddingPrice: 15, TargetKPIValue: 1.0},
		config.DefaultPricingBenchmark(), testNow, 0, 100)
	okC := e.scoreCreative(mkCr(&config.Advertiser{ID: "ok"}),
		&config.Advertiser{ID: "ok"},
		&config.Campaign{BillingMode: "cpm", BiddingPrice: 15, TargetKPIValue: 1.0},
		config.DefaultPricingBenchmark(), testNow, 0, 100)

	if math.Abs(newC.score-okC.score) > 1e-9 {
		t.Fatalf("冷启动广告主应与达标者同分（中性处理），实际 new=%v ok=%v", newC.score, okC.score)
	}
}

func TestDecide_冷启动批量不独吞(t *testing.T) {
	// 冷启动新广告主 + 2 个老广告主，count=3：候选仅 3 个素材，各出现一次
	snap := mkSnapshot(
		mkAdv("new", 1, 1.0, 0),   // 冷启动
		mkAdv("urg", 1, 1.0, 2.0), // 达成率 50%，最紧急
		mkAdv("ok", 1, 1.0, 1.0),  // 达成率 100%
	)
	req := Request{App: snap.Apps["app1"], Style: "rewarded_video",
		DeviceID: "d1", Count: 3, Now: testNow}
	resp := mkEngine(&fakeFreq{}, mkBudget()).Decide(snap, req)
	if len(resp.Items) != 3 {
		t.Fatalf("count=3 候选 3 个应返回 3 条，实际 %d", len(resp.Items))
	}
	counts := map[string]int{}
	for _, it := range resp.Items {
		counts[it.AdvertiserID]++
	}
	for _, id := range []string{"new", "urg", "ok"} {
		if counts[id] != 1 {
			t.Fatalf("每个广告主素材应各出现 1 次，%s 出现 %d 次（items=%v）", id, counts[id], ids(resp.Items))
		}
	}
}

func TestDecide_下发有效期expire_at(t *testing.T) {
	// 未配置（0）→ 兜底 10 分钟；配置 5 → 5 分钟。expire_at = now + ttl。
	withDefault := mkAdv("def", 1, 1.0, 1.0) // camp.DeliverTTLMinutes=0
	custom := mkAdv("cus", 1, 1.0, 1.0)
	custom.camp.DeliverTTLMinutes = 5

	snap := mkSnapshot(withDefault, custom)
	// 两个广告主都进名单，count=2 返回两条，分别验证 ttl
	req := Request{App: snap.Apps["app1"], Style: "rewarded_video",
		DeviceID: "d1", Count: 2, Now: testNow}
	resp := mkEngine(&fakeFreq{}, mkBudget()).Decide(snap, req)
	if len(resp.Items) != 2 {
		t.Fatalf("count=2 应返回 2 条，实际 %d", len(resp.Items))
	}
	wantDef := testNow.Add(10 * time.Minute).Unix()
	wantCus := testNow.Add(5 * time.Minute).Unix()
	for _, it := range resp.Items {
		switch it.AdvertiserID {
		case "def":
			if it.ExpireAt != wantDef {
				t.Fatalf("未配置应兜底 10 分钟，expire_at=%d 期望 %d", it.ExpireAt, wantDef)
			}
		case "cus":
			if it.ExpireAt != wantCus {
				t.Fatalf("配置 5 分钟应生效，expire_at=%d 期望 %d", it.ExpireAt, wantCus)
			}
		}
	}
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
