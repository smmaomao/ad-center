package api

import (
	"context"
	"testing"
)

// TestMemBidStore_GetAndConsume 验证合并版「取上下文 + 幂等标记」按
// (bid_id, event_type) 各记一次：首次命中且标记、重放返回原上下文但
// first=false、不同事件类型/不同 bid 各自可首次通过、Unconsume 回退后
// 可再次标记、不存在的 bid 返回 nil。
func TestMemBidStore_GetAndConsume(t *testing.T) {
	s := NewMemBidStore()
	id := s.Put(context.Background(), &BidContext{CreativeID: "c1"})

	bid, first, err := s.GetAndConsume(context.Background(), id, "impression")
	if err != nil || bid == nil || bid.CreativeID != "c1" || !first {
		t.Fatalf("first GetAndConsume should hit context and be first, got (%v,%v,%v)", bid, first, err)
	}
	bid, first, _ = s.GetAndConsume(context.Background(), id, "impression")
	if bid == nil || first {
		t.Fatal("replay should return context with first=false")
	}
	// 不同事件类型可各计一次
	if _, first, _ := s.GetAndConsume(context.Background(), id, "click"); !first {
		t.Fatal("different event type on same bid should be first")
	}
	// 校验失败补偿：Unconsume 后同一事件可再次标记
	s.Unconsume(context.Background(), id, "impression")
	if _, first, _ := s.GetAndConsume(context.Background(), id, "impression"); !first {
		t.Fatal("after Unconsume the event slot should be reusable")
	}
	// 不同 bid 相互独立
	id2 := s.Put(context.Background(), &BidContext{CreativeID: "c2"})
	if _, first, _ := s.GetAndConsume(context.Background(), id2, "impression"); !first {
		t.Fatal("different bid should be first")
	}
	// 不存在的 bid
	if bid, _, _ := s.GetAndConsume(context.Background(), "nope", "impression"); bid != nil {
		t.Fatal("missing bid should return nil context")
	}
}
