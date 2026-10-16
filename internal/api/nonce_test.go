package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"adcenter/internal/config"
	"adcenter/internal/queue"
)

func slogDiscard() *slog.Logger { return slog.New(slog.DiscardHandler) }

func newTestApp(t *testing.T) (*Server, string) {
	t.Helper()
	const apiKey = "adc_test_0000000000000000000000000009"
	sum := sha256.Sum256([]byte(apiKey))
	keyHash := hex.EncodeToString(sum[:])

	snap := &config.Snapshot{
		Apps:         map[string]*config.App{},
		AppByKeyHash: map[string]*config.App{},
	}
	app := &config.App{ID: "app_t", Status: "active"}
	snap.Apps[app.ID] = app
	snap.AppByKeyHash[keyHash] = app

	cache, err := config.NewCache(context.Background(), &fixedLoader{snap}, slogDiscard())
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{
		Cache: cache,
		Log:   slogDiscard(),
		Nonce: NewMemNonceStore(5 * time.Minute),
		Queue: queue.NewMemory(1024, 50, time.Second),
	}
	return s, apiKey
}

func TestSignReward(t *testing.T) {
	a := signReward("k", "app", "u1", "adj1", "n1")
	b := signReward("k", "app", "u1", "adj1", "n1")
	if a != b {
		t.Fatal("sign not deterministic")
	}
	if signReward("k", "app", "u1", "adj2", "n1") == a {
		t.Fatal("adjust_adid must affect sign")
	}
	if signReward("k2", "app", "u1", "adj1", "n1") == a {
		t.Fatal("secret must affect sign")
	}
	if signReward("k", "app", "u1", "adj1", "n2") == a {
		t.Fatal("nonce must affect sign")
	}
}

func TestMemNonceSingleUse(t *testing.T) {
	ns := NewMemNonceStore(5 * time.Minute)
	nonce, _, err := ns.Issue("app_t")
	if err != nil {
		t.Fatal(err)
	}
	if !ns.Consume("app_t", nonce) {
		t.Fatal("first consume should succeed")
	}
	if ns.Consume("app_t", nonce) {
		t.Fatal("second consume must fail (replay)")
	}
	if ns.Consume("other_app", nonce) {
		t.Fatal("nonce must be bound to issuing app")
	}
}

func TestSignedVideoCompleteFlow(t *testing.T) {
	s, apiKey := newTestApp(t)

	// ① 取 nonce
	nr := httptest.NewRequest("POST", "/v1/ad/nonce", nil)
	nr.Header.Set("X-Api-Key", apiKey)
	nw := httptest.NewRecorder()
	s.handleGetNonce(nw, nr)
	if nw.Code != http.StatusOK {
		t.Fatalf("get nonce code=%d body=%s", nw.Code, nw.Body.String())
	}
	var nb struct {
		Nonce string `json:"nonce"`
	}
	_ = json.Unmarshal(nw.Body.Bytes(), &nb)
	if nb.Nonce == "" {
		t.Fatal("empty nonce")
	}

	// ② 正常签名回调（签名密钥 = 请求所用的 X-Api-Key）
	sign := signReward(apiKey, "app_t", "u1", "adj1", nb.Nonce)
	body := `{"ad_app_id":"app_t","user_id":"u1","adjust_adid":"adj1","nonce":"` + nb.Nonce + `","sign":"` + sign + `"}`
	req := httptest.NewRequest("POST", "/v1/ad/video-complete-signed", strings.NewReader(body))
	req.Header.Set("X-Api-Key", apiKey)
	w := httptest.NewRecorder()
	s.handleAdVideoCompleteSigned(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("signed complete code=%d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		AdAppID    string `json:"ad_app_id"`
		UserID     string `json:"user_id"`
		AdjustAdid string `json:"adjust_adid"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.AdAppID != "app_t" || resp.UserID != "u1" || resp.AdjustAdid != "adj1" {
		t.Fatalf("echoed fields wrong: %+v", resp)
	}

	// ③ 重放同一 nonce → 拒绝
	req2 := httptest.NewRequest("POST", "/v1/ad/video-complete-signed", strings.NewReader(body))
	req2.Header.Set("X-Api-Key", apiKey)
	w2 := httptest.NewRecorder()
	s.handleAdVideoCompleteSigned(w2, req2)
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("replay should be 400, got %d", w2.Code)
	}

	// ④ 篡改签名 → 拒绝
	bad := `{"ad_app_id":"app_t","user_id":"u1","adjust_adid":"adj1","nonce":"` + nb.Nonce + `","sign":"deadbeef"}`
	req3 := httptest.NewRequest("POST", "/v1/ad/video-complete-signed", strings.NewReader(bad))
	req3.Header.Set("X-Api-Key", apiKey)
	w3 := httptest.NewRecorder()
	s.handleAdVideoCompleteSigned(w3, req3)
	if w3.Code != http.StatusUnauthorized {
		t.Fatalf("bad sign should be 401, got %d body=%s", w3.Code, w3.Body.String())
	}

	// ⑤ ad_app_id 冒用他人 → 拒绝
	sign2 := signReward(apiKey, "other", "u1", "adj1", "x")
	bad2 := `{"ad_app_id":"other","user_id":"u1","adjust_adid":"adj1","nonce":"x","sign":"` + sign2 + `"}`
	req4 := httptest.NewRequest("POST", "/v1/ad/video-complete-signed", strings.NewReader(bad2))
	req4.Header.Set("X-Api-Key", apiKey)
	w4 := httptest.NewRecorder()
	s.handleAdVideoCompleteSigned(w4, req4)
	if w4.Code != http.StatusBadRequest {
		t.Fatalf("ad_app_id mismatch should be 400, got %d", w4.Code)
	}
}
