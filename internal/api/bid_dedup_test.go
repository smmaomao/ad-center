package api

import (
	"context"
	"testing"
)

// TestMemBidStore_ConsumeIdempotent 验证按 (bid_id, event_type) 各记一次：
// 同一组合重放返回 false；不同事件类型 / 不同 bid 各自可首次通过。
func TestMemBidStore_ConsumeIdempotent(t *testing.T) {
	s := NewMemBidStore()

	if ok, _ := s.Consume(context.Background(), "b1", "impression"); !ok {
		t.Fatal("first consume should be true")
	}
	if ok, _ := s.Consume(context.Background(), "b1", "impression"); ok {
		t.Fatal("second consume of same (bid,event) must be false (replay)")
	}
	// 不同事件类型可各计一次
	if ok, _ := s.Consume(context.Background(), "b1", "click"); !ok {
		t.Fatal("different event type on same bid should be true")
	}
	if ok, _ := s.Consume(context.Background(), "b1", "click"); ok {
		t.Fatal("second click on same bid must be false")
	}
	// 不同 bid 相互独立
	if ok, _ := s.Consume(context.Background(), "b2", "impression"); !ok {
		t.Fatal("different bid should be true")
	}
}
