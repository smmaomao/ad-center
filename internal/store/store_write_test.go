package store

import (
	"encoding/json"
	"testing"
)

// TestAdEvent_EventUIDRoundTrip 锁住「事件级幂等键随事件经 JSON 序列化穿过 Redis
// Stream 后仍完好」这一契约：落库去重（event_dedup）依赖 EventUID 在重投时保持一致。
func TestAdEvent_EventUIDRoundTrip(t *testing.T) {
	e := AdEvent{
		AppID:     "app_1",
		EventType: "impression",
		EventUID:  "ev_AaBbCcDdEeFfGgHhIiJj",
	}
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	var got AdEvent
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.EventUID != e.EventUID {
		t.Fatalf("EventUID lost in JSON round-trip: got %q want %q", got.EventUID, e.EventUID)
	}
}
