package api

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"adcenter/internal/queue"
	"adcenter/internal/store"
)

// fakeConvChecker 可控的转化去重查询：exists 决定"是否已记过账"。
type fakeConvChecker struct {
	exists bool
	calls  int
	lastID string
	lastEv string
}

func (f *fakeConvChecker) ConversionExists(_ context.Context, clickID, eventType string) (bool, error) {
	f.calls++
	f.lastID = clickID
	f.lastEv = eventType
	return f.exists, nil
}

// spyQueue 记录 Publish 次数的事件队列（不真正消费）。
type spyQueue struct{ published int }

func (q *spyQueue) Publish(e store.AdEvent)            { q.published++ }
func (q *spyQueue) Run(context.Context, queue.Handler) {}
func (q *spyQueue) Stats() queue.Stats                 { return queue.Stats{} }
func (q *spyQueue) Close() error                       { return nil }

// TestS2SEvent_DuplicateSkipped 同一 (click_id, event_type) 已记过账 → 确认收到、
// 不扣费、不入队（防归因平台重试 / 重放重复扣款）。
func TestS2SEvent_DuplicateSkipped(t *testing.T) {
	chk := &fakeConvChecker{exists: true} // 已记过账
	q := &spyQueue{}
	s := &Server{
		Log:         slog.New(slog.DiscardHandler),
		Conversions: chk,
		Clicks: fakeClickResolver{m: map[string]*ClickContext{"c1": {
			AppID: "a", AdvertiserID: "adv", CampaignID: "c", CreativeID: "cr", DeviceID: "d",
		}}},
		Queue: q,
	}
	r := httptest.NewRequest(http.MethodGet, "/v1/s2s/event?clickid=c1&event_name=install", nil)
	w := httptest.NewRecorder()
	s.handleS2SEvent(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	if chk.calls != 1 || chk.lastID != "c1" || chk.lastEv != "install" {
		t.Fatalf("应查询去重，got calls=%d id=%s ev=%s", chk.calls, chk.lastID, chk.lastEv)
	}
	if q.published != 0 {
		t.Fatalf("重复转化不应入队，got %d", q.published)
	}
}

// TestS2SEvent_UnattributableNoCharge clickid 无法反查 → 确认收到、不扣费、不查去重、
// 不入队（防归因方把失败当重试放大刷量）。
func TestS2SEvent_UnattributableNoCharge(t *testing.T) {
	chk := &fakeConvChecker{exists: false}
	q := &spyQueue{}
	s := &Server{
		Log:         slog.New(slog.DiscardHandler),
		Conversions: chk,
		Clicks:      fakeClickResolver{m: map[string]*ClickContext{}}, // 空表 → 任何 clickid 都查不到
		Queue:       q,
	}
	r := httptest.NewRequest(http.MethodGet, "/v1/s2s/event?clickid=c1&event_name=install", nil)
	w := httptest.NewRecorder()
	s.handleS2SEvent(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	if chk.calls != 0 {
		t.Fatalf("未归因时不应查询去重，got calls=%d", chk.calls)
	}
	if q.published != 0 {
		t.Fatalf("未归因不应入队，got %d", q.published)
	}
}
