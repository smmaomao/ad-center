package engine

import (
	"testing"
	"time"

	"adcenter/internal/budget"
	"adcenter/internal/config"
	"adcenter/internal/frequency"
)

// ===== 测试替身 =====

type fakeFreq struct {
	denyAdv map[string]bool // 拒绝这些广告主（CheckAndIncr）
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

type fakeBudget struct {
	spent  map[string]float64
	budget map[string]float64
	deny   map[string]bool
	deduct []string // TryDeduct 成功记录
}

func (b *fakeBudget) TryDeduct(advID string, amount float64) bool {
	if b.deny[advID] {
		return false
	}
	b.spent[advID] += amount
	b.deduct = append(b.deduct, advID)
	return true
}
func (b *fakeBudget) Commit(string, float64) error   { return nil }
func (b *fakeBudget) Rollback(string, float64) error { return nil }
func (b *fakeBudget) Stats(advID string) (float64, float64) {
	return b.spent[advID], b.budget[advID]
}
func (b *fakeBudget) HourlyCalibrate() error { return nil }

var _ budget.Ctrl = (*fakeBudget)(nil)

// ===== 测试工具 =====

var testNow = time.Date(2026, 9, 4, 3, 0, 0, 0, time.UTC) // Jakarta 10:00（进度 10/24≈0.417）

func mkSnapshot(advs ...*config.Advertiser) *config.Snapshot {
	snap := &config.Snapshot{
		Apps:                  map[string]*config.App{"app1": {ID: "app1", Status: "active"}},
		Advertisers:           map[string]*config.Advertiser{},
		Slots:                 map[string]*config.Slot{},
		SlotsByKey:            map[string]*config.Slot{},
		CreativesByAdvertiser: map[string][]*config.Creative{},
	}
	for _, a := range advs {
		snap.Advertisers[a.ID] = a
		snap.CreativesByAdvertiser[a.ID] = []*config.Creative{{
			ID: "cr_" + a.ID, AdvertiserID: a.ID, Status: "active",
			MediaType: "video", Weight: 1,
		}}
	}
	return snap
}

func mkAdv(id string, tier int, target, actual, bid float64) *config.Advertiser {
	return &config.Advertiser{ID: id, Name: id, Tier: tier, Status: "active",
		TargetCPI: target, ActualCPI: actual, BiddingPrice: bid, DailyBudget: 1000}
}

func mkSlot(advIDs ...string) *config.Slot {
	s := &config.Slot{ID: "slot1", AppID: "app1", Key: "test_slot", Status: "active",
		FreqDailyLimit: 8, FreqIntervalMinutes: 20, FreqFatigueWindow: 3}
	for i, id := range advIDs {
		s.Priorities = append(s.Priorities, config.FillPriority{
			ID: string(rune('a' + i)), SourceType: "advertiser", AdvertiserID: id, Weight: 1,
		})
	}
	return s
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

func TestDecideSingle_KPI紧急度排序(t *testing.T) {
	// adv_urgent 达成率 0.5（紧急），adv_ok 达成率 1.0（达优），其他同条件
	snap := mkSnapshot(
		mkAdv("adv_urgent", 1, 1.0, 0.5, 1.0),
		mkAdv("adv_ok", 1, 1.0, 1.0, 1.0),
	)
	req := Request{App: snap.Apps["app1"], Slot: mkSlot("adv_ok", "adv_urgent"),
		DeviceID: "d1", Count: 1, Now: testNow}
	resp := mkEngine(&fakeFreq{denyAdv: map[string]bool{}}, mkBudget()).Decide(snap, req)
	if len(resp.Items) != 1 || resp.Items[0].AdvertiserID != "adv_urgent" {
		t.Fatalf("KPI 紧急广告主应胜出，实际 %v", ids(resp.Items))
	}
}

func TestDecideSingle_Tier权重(t *testing.T) {
	// 同达成率下 Tier1 > Tier2 > Tier3
	snap := mkSnapshot(
		mkAdv("t3", 3, 1.0, 0.9, 1.0),
		mkAdv("t1", 1, 1.0, 0.9, 1.0),
		mkAdv("t2", 2, 1.0, 0.9, 1.0),
	)
	req := Request{App: snap.Apps["app1"], Slot: mkSlot("t3", "t2", "t1"),
		DeviceID: "d1", Count: 1, Now: testNow}
	resp := mkEngine(&fakeFreq{}, mkBudget()).Decide(snap, req)
	if resp.Items[0].AdvertiserID != "t1" {
		t.Fatalf("Tier1 应胜出，实际 %v", ids(resp.Items))
	}
}

func TestDecide_消耗节奏系数(t *testing.T) {
	// Jakarta 10:00 预期进度 ≈ 0.417
	// adv_slow 实际 0.1（落后 >10%）→ ×1.3；adv_fast 实际 0.9（超前）→ ×0.8
	// 两者达成率相同，slow 应胜出
	snap := mkSnapshot(
		mkAdv("slow", 1, 1.0, 0.9, 1.0),
		mkAdv("fast", 1, 1.0, 0.9, 1.0),
	)
	bud := mkBudget()
	bud.budget["slow"], bud.spent["slow"] = 100, 10
	bud.budget["fast"], bud.spent["fast"] = 100, 90
	req := Request{App: snap.Apps["app1"], Slot: mkSlot("fast", "slow"),
		DeviceID: "d1", Count: 1, Now: testNow}
	resp := mkEngine(&fakeFreq{}, bud).Decide(snap, req)
	if resp.Items[0].AdvertiserID != "slow" {
		t.Fatalf("消耗偏慢者应加速胜出，实际 %v", ids(resp.Items))
	}
}

func TestDecideSingle_疲劳过滤顺延(t *testing.T) {
	// 最高分 adv1 被频控拒绝 → 顺延 adv2（PRD 5.1：跳过继续）
	snap := mkSnapshot(
		mkAdv("adv1", 1, 1.0, 0.5, 1.0),
		mkAdv("adv2", 1, 1.0, 0.9, 1.0),
	)
	freq := &fakeFreq{denyAdv: map[string]bool{"adv1": true}}
	req := Request{App: snap.Apps["app1"], Slot: mkSlot("adv1", "adv2"),
		DeviceID: "d1", Count: 1, Now: testNow}
	resp := mkEngine(freq, mkBudget()).Decide(snap, req)
	if len(resp.Items) != 1 || resp.Items[0].AdvertiserID != "adv2" {
		t.Fatalf("应顺延到 adv2，实际 %v", ids(resp.Items))
	}
}

func TestDecideSingle_预算不足顺延(t *testing.T) {
	snap := mkSnapshot(
		mkAdv("adv1", 1, 1.0, 0.5, 5.0),
		mkAdv("adv2", 1, 1.0, 0.9, 3.0),
	)
	bud := mkBudget()
	bud.deny["adv1"] = true
	req := Request{App: snap.Apps["app1"], Slot: mkSlot("adv1", "adv2"),
		DeviceID: "d1", Count: 1, Now: testNow}
	resp := mkEngine(&fakeFreq{}, bud).Decide(snap, req)
	if resp.Items[0].AdvertiserID != "adv2" {
		t.Fatalf("预算不足应顺延 adv2，实际 %v", ids(resp.Items))
	}
	if len(bud.deduct) != 1 || bud.deduct[0] != "adv2" {
		t.Fatalf("只应扣减 adv2，实际 %v", bud.deduct)
	}
}

func TestDecide_定向过滤(t *testing.T) {
	adv := mkAdv("adv_id", 1, 1.0, 0.9, 1.0)
	adv.Targeting = config.Targeting{Countries: []string{"ID"}}
	snap := mkSnapshot(adv)
	req := Request{App: snap.Apps["app1"], Slot: mkSlot("adv_id"),
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

func TestDecide_投放截止过滤(t *testing.T) {
	end := testNow.Add(-time.Hour)
	adv := mkAdv("adv_end", 1, 1.0, 0.9, 1.0)
	adv.EndAt = &end
	snap := mkSnapshot(adv)
	req := Request{App: snap.Apps["app1"], Slot: mkSlot("adv_end"),
		DeviceID: "d1", Count: 1, Now: testNow}
	resp := mkEngine(&fakeFreq{}, mkBudget()).Decide(snap, req)
	if resp.Fallback == "" {
		t.Fatal("已过投放截止时间应被过滤")
	}
}

func TestDecide_无可用降级链(t *testing.T) {
	snap := mkSnapshot(mkAdv("adv1", 1, 1.0, 0.9, 1.0))
	// slot 配置了 max 来源 → fallback=max
	slot := mkSlot("adv1")
	slot.Priorities = append(slot.Priorities, config.FillPriority{SourceType: "max"})
	req := Request{App: snap.Apps["app1"], Slot: slot, DeviceID: "d1", Count: 1, Now: testNow}
	resp := mkEngine(&fakeFreq{denyAll: true}, mkBudget()).Decide(snap, req)
	if resp.Fallback != "max" {
		t.Fatalf("无填充应降级 max，实际 %q", resp.Fallback)
	}
	// 无 max 来源 → self_promo
	req.Slot = mkSlot("adv1")
	resp = mkEngine(&fakeFreq{denyAll: true}, mkBudget()).Decide(snap, req)
	if resp.Fallback != "self_promo" {
		t.Fatalf("无 max 配置应 self_promo，实际 %q", resp.Fallback)
	}
}

func TestDecideBatch_topN与整轮下发(t *testing.T) {
	// 5 个候选，达成率各不相同（分数梯度）；出价各异（轮内排序用）
	snap := mkSnapshot(
		mkAdv("a", 1, 1.0, 0.5, 2.0), // 最高分
		mkAdv("b", 1, 1.0, 0.7, 3.0),
		mkAdv("c", 2, 1.0, 0.9, 1.0),
		mkAdv("d", 2, 1.0, 1.0, 4.0),
		mkAdv("e", 3, 1.0, 1.2, 2.5),
	)
	slot := mkSlot("a", "b", "c", "d", "e")
	e := mkEngine(&fakeFreq{}, mkBudget())

	// count=3：取分数最高 3 个（a,b,c），轮内按出价降序 b(3)>a(2)>c(1)
	req := Request{App: snap.Apps["app1"], Slot: slot, DeviceID: "d1", Count: 3, Now: testNow}
	resp := e.Decide(snap, req)
	if len(resp.Items) != 3 {
		t.Fatalf("count=3 应返回 3 条，实际 %d", len(resp.Items))
	}
	got := ids(resp.Items)
	// 得分：a=1/0.51×0.8×1.5≈2.353，b=1/0.71×0.8×1.5≈1.690，
	//       c=1/0.91×1.2×1.2≈1.582，d=1/1.01×1.2×1.2≈1.426，e=1/1.21×1.2×1.0≈0.992
	// 名单 = 分数 top3（a,b,c）；轮内按出价降序：b(3) > a(2) > c(1)
	want := []string{"b", "a", "c"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("count=3 名单/顺序错误：got %v want %v", got, want)
		}
	}

	// count=10：5 候选 × 2 整轮；每轮内按出价降序 d(4)>e(2.5)>b(3)？
	// 出价：a=2,b=3,c=1,d=4,e=2.5 → 轮内序 = d,b,e,a,c
	req.Count = 10
	resp = e.Decide(snap, req)
	if len(resp.Items) != 10 {
		t.Fatalf("count=10 应返回 10 条，实际 %d", len(resp.Items))
	}
	want10 := []string{"d", "b", "e", "a", "c", "d", "b", "e", "a", "c"}
	got10 := ids(resp.Items)
	for i := range want10 {
		if got10[i] != want10[i] {
			t.Fatalf("count=10 轮次序错误：\n got  %v\n want %v", got10, want10)
		}
	}
}

func TestDecideBatch_保底强制换入(t *testing.T) {
	// 3 候选：a/b 分高，c 分低但保底 40%
	snap := mkSnapshot(
		mkAdv("a", 1, 1.0, 0.5, 1.0),
		mkAdv("b", 1, 1.0, 0.7, 1.0),
		mkAdv("c", 3, 1.0, 2.0, 1.0), // 达成率 200%，分数最低
	)
	slot := mkSlot("a", "b", "c")
	slot.Priorities[2].GuaranteedShare = 0.4 // c 保底 40%
	req := Request{App: snap.Apps["app1"], Slot: slot, DeviceID: "d1", Count: 5, Now: testNow}
	resp := mkEngine(&fakeFreq{}, mkBudget()).Decide(snap, req)
	if len(resp.Items) != 5 {
		t.Fatalf("应返回 5 条，实际 %d", len(resp.Items))
	}
	// ceil(5×0.4)=2：c 应出现 2 次
	countC := 0
	for _, it := range resp.Items {
		if it.AdvertiserID == "c" {
			countC++
		}
	}
	if countC != 2 {
		t.Fatalf("保底 40%% × count 5 = ceil 2，c 实际出现 %d 次（items=%v）", countC, ids(resp.Items))
	}
}

func TestDecideBatch_物化失败不补位(t *testing.T) {
	snap := mkSnapshot(
		mkAdv("a", 1, 1.0, 0.5, 1.0),
		mkAdv("b", 1, 1.0, 0.7, 1.0),
	)
	freq := &fakeFreq{denyAdv: map[string]bool{"b": true}}
	req := Request{App: snap.Apps["app1"], Slot: mkSlot("a", "b"),
		DeviceID: "d1", Count: 4, Now: testNow}
	resp := mkEngine(freq, mkBudget()).Decide(snap, req)
	// 2 轮 × 2 = 4 条，但 b 两条都被拒 → 只剩 a×2，不补位
	if len(resp.Items) != 2 {
		t.Fatalf("b 被频控拒绝应只返回 a×2，实际 %v", ids(resp.Items))
	}
	for _, it := range resp.Items {
		if it.AdvertiserID != "a" {
			t.Fatalf("不应包含 b，实际 %v", ids(resp.Items))
		}
	}
}

func TestDecideBatch_count上限(t *testing.T) {
	snap := mkSnapshot(mkAdv("a", 1, 1.0, 0.9, 1.0))
	req := Request{App: snap.Apps["app1"], Slot: mkSlot("a"),
		DeviceID: "d1", Count: 999, Now: testNow}
	resp := mkEngine(&fakeFreq{}, mkBudget()).Decide(snap, req)
	if len(resp.Items) != MaxCount {
		t.Fatalf("count 应被钳制到 %d，实际 %d", MaxCount, len(resp.Items))
	}
}

func TestDecide_无活跃素材降级(t *testing.T) {
	snap := mkSnapshot(mkAdv("a", 1, 1.0, 0.9, 1.0))
	// 素材置为 paused
	snap.CreativesByAdvertiser["a"][0].Status = "paused"
	req := Request{App: snap.Apps["app1"], Slot: mkSlot("a"), DeviceID: "d1", Count: 1, Now: testNow}
	resp := mkEngine(&fakeFreq{}, mkBudget()).Decide(snap, req)
	if resp.Fallback == "" {
		t.Fatal("无活跃素材应降级")
	}
}

func TestScore_消耗节奏边界(t *testing.T) {
	// 纯函数级：dayProgress 与 ±10% 阈值
	bud := &fakeBudget{spent: map[string]float64{}, budget: map[string]float64{}}
	adv := mkAdv("x", 1, 1.0, 0.9, 1.0)
	e := &Engine{Budget: bud}

	// Jakarta 10:00 → 预期 ≈ 0.4167
	// 实际 0.35（差 6.7%）→ 正常
	bud.budget["x"], bud.spent["x"] = 100, 35
	if got := e.pacingFactor(adv, testNow); got != 1.0 {
		t.Fatalf("偏差小于10%%时应为 1.0，实际 %v", got)
	}
	// 实际 0.30（差 11.7%）→ 偏慢加速
	bud.spent["x"] = 30
	if got := e.pacingFactor(adv, testNow); got != 1.3 {
		t.Fatalf("落后超过10%%应为 1.3，实际 %v", got)
	}
	// 实际 0.55（超前 13.3%）→ 偏快减速
	bud.spent["x"] = 55
	if got := e.pacingFactor(adv, testNow); got != 0.8 {
		t.Fatalf("超前超过10%%应为 0.8，实际 %v", got)
	}
}

func TestDecide_请求内可复现(t *testing.T) {
	// 同输入同输出（排序稳定性 + 确定性 tie-break）
	snap := mkSnapshot(
		mkAdv("x", 1, 1.0, 0.9, 1.0),
		mkAdv("y", 1, 1.0, 0.9, 1.0), // 与 x 完全同分
	)
	slot := mkSlot("y", "x")
	e := mkEngine(&fakeFreq{}, mkBudget())
	req := Request{App: snap.Apps["app1"], Slot: slot, DeviceID: "d1", Count: 1, Now: testNow}
	first := ids(e.Decide(snap, req).Items)
	for range 10 {
		got := ids(e.Decide(snap, req).Items)
		if got[0] != first[0] {
			t.Fatalf("同输入应同输出：首次 %v，本次 %v", first, got)
		}
	}
	// 平局按 advertiser_id 升序 → x 胜出
	if first[0] != "x" {
		t.Fatalf("平局应按 id 升序选 x，实际 %v", first)
	}
}
