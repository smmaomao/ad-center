package api

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// NonceStore 一次性 nonce 的发放 / 核销（防重放）。
//
// 背景：激励视频完播回调原本只带 user_id，脚本可无限重放。引入「服务端发放
// nonce + 客户端对 (ad_app_id|user_id|adjust_adid|nonce) 做 HMAC 签名」后，
// 每条有效回调都必须用服务端刚发的、且只能用一次的 nonce —— nonce 核销即废，
// 抓包重放直接 400；伪造签名还需 App 的 secret_key，curl 脚本拿不到。
type NonceStore interface {
	// Issue 发放一个绑定 appID 的 nonce（单次有效），返回 nonce 与有效期。
	Issue(appID string) (nonce string, ttl time.Duration, err error)
	// Consume 核销：存在且未用过、且归属该 appID → 返回 true 并作废；否则 false。
	Consume(appID, nonce string) bool
}

const nonceKeyPrefix = "adcenter:nonce:"

// ---------------------------------------------------------------------------
// Redis 实现（生产，多实例共享）
// ---------------------------------------------------------------------------

type redisNonceStore struct {
	c   *redis.Client
	ttl time.Duration
}

func NewRedisNonceStore(c *redis.Client, ttl time.Duration) NonceStore {
	return &redisNonceStore{c: c, ttl: ttl}
}

func (s *redisNonceStore) Issue(appID string) (string, time.Duration, error) {
	ctx := context.Background()
	nonce := randHex(16)
	// SET NX EX：同一 nonce 只能成功发放一次；value=appID 做归属绑定。
	if err := s.c.SetNX(ctx, nonceKeyPrefix+nonce, appID, s.ttl).Err(); err != nil {
		return "", 0, err
	}
	return nonce, s.ttl, nil
}

// consumeScript 原子「读归属并删除」，保证并发下同一 nonce 只被核销一次。
var consumeScript = redis.NewScript(`
if redis.call('exists', KEYS[1]) == 0 then return 0 end
if redis.call('get', KEYS[1]) ~= ARGV[1] then return -1 end
redis.call('del', KEYS[1])
return 1`)

func (s *redisNonceStore) Consume(appID, nonce string) bool {
	ctx := context.Background()
	res, err := consumeScript.Run(ctx, s.c, []string{nonceKeyPrefix + nonce}, appID).Int64()
	if err != nil {
		return false
	}
	return res == 1
}

// ---------------------------------------------------------------------------
// 内存实现（本地开发 / 无 REDIS_URL 时降级）
// ---------------------------------------------------------------------------

type memNonceStore struct {
	mu  sync.Mutex
	m   map[string]nonceEntry
	ttl time.Duration
}

type nonceEntry struct {
	appID string
	exp   time.Time
}

func NewMemNonceStore(ttl time.Duration) NonceStore {
	return &memNonceStore{m: make(map[string]nonceEntry), ttl: ttl}
}

func (s *memNonceStore) Issue(appID string) (string, time.Duration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweep()
	nonce := randHex(16)
	for {
		if _, ok := s.m[nonce]; !ok {
			break
		}
		nonce = randHex(16)
	}
	s.m[nonce] = nonceEntry{appID: appID, exp: time.Now().Add(s.ttl)}
	return nonce, s.ttl, nil
}

func (s *memNonceStore) Consume(appID, nonce string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweep()
	e, ok := s.m[nonce]
	if !ok {
		return false
	}
	if e.appID != appID {
		delete(s.m, nonce)
		return false
	}
	delete(s.m, nonce)
	return true
}

func (s *memNonceStore) sweep() {
	now := time.Now()
	for k, e := range s.m {
		if now.After(e.exp) {
			delete(s.m, k)
		}
	}
}

// ---------------------------------------------------------------------------
// 签名
// ---------------------------------------------------------------------------

// signReward 对 (ad_app_id|user_id|adjust_adid|nonce) 做 HMAC-SHA256（密钥 = App
// 的 secret_key）。nonce 必须纳入签名，否则攻击者可拿一个旧 sign 配服务端新发的
// nonce 复用（nonce 虽单次有效但 sign 不绑 nonce → 仍能持续伪造）。
func signReward(secret, adAppID, userID, adjustAdid, nonce string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strings.Join([]string{adAppID, userID, adjustAdid, nonce}, "|")))
	return hex.EncodeToString(mac.Sum(nil))
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ---------------------------------------------------------------------------
// 路由处理
// ---------------------------------------------------------------------------

// handleGetNonce 发放一次性 nonce：客户端在发起「看完广告」回调前先取。
func (s *Server) handleGetNonce(w http.ResponseWriter, r *http.Request) {
	app := s.authenticateApp(w, r)
	if app == nil {
		return
	}
	if s.Nonce == nil {
		writeError(w, http.StatusServiceUnavailable, "nonce store not configured")
		return
	}
	nonce, ttl, err := s.Nonce.Issue(app.ID)
	if err != nil {
		s.Log.Error("issue nonce failed", "app", app.ID, "err", err)
		writeError(w, http.StatusInternalServerError, "issue nonce failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"nonce":       nonce,
		"ttl_seconds": int(ttl.Seconds()),
	})
}

// handleAdVideoCompleteSigned 带签名 + nonce 的激励视频完播回调（防重放 / 防伪造）。
//
// 与 /v1/ad/video-complete 的区别：必须携带服务端刚发的 nonce 与对
// (ad_app_id|user_id|adjust_adid|nonce) 的 HMAC 签名（密钥 = App 的 secret_key）。
// 校验顺序：① ad_app_id 与鉴权 App 一致 → ② 签名恒定时间比对 → ③ nonce 核销
// （顺序很关键：签名不过不消耗 nonce，避免有人用合法 api_key 批量领 nonce 来"烧"
// 合法客户端的 nonce 池）。全部通过才 fireRewardCallback，并原样回带三个字段。
func (s *Server) handleAdVideoCompleteSigned(w http.ResponseWriter, r *http.Request) {
	app := s.authenticateApp(w, r)
	if app == nil {
		return
	}
	var req struct {
		AdAppID    string `json:"ad_app_id"`
		UserID     string `json:"user_id"`
		AdjustAdid string `json:"adjust_adid"`
		Nonce      string `json:"nonce"`
		Sign       string `json:"sign"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.UserID == "" {
		writeError(w, http.StatusBadRequest, "user_id required")
		return
	}
	if s.Nonce == nil {
		writeError(w, http.StatusServiceUnavailable, "nonce store not configured")
		return
	}
	if req.AdAppID != app.ID {
		// ad_app_id 由鉴权推导，客户端不可冒用他人 App。
		writeError(w, http.StatusBadRequest, "ad_app_id mismatch")
		return
	}
	// ① 签名恒定时间比对（先验签名，不消耗 nonce）。
	expect := signReward(app.SecretKey, req.AdAppID, req.UserID, req.AdjustAdid, req.Nonce)
	if subtle.ConstantTimeCompare([]byte(expect), []byte(req.Sign)) != 1 {
		writeError(w, http.StatusUnauthorized, "invalid signature")
		return
	}
	// ② nonce 核销：不存在 / 已用 / 归属不符 → 拒绝（重放即死）。
	if !s.Nonce.Consume(app.ID, req.Nonce) {
		writeError(w, http.StatusBadRequest, "invalid or expired nonce")
		return
	}
	// 校验通过 → 触发发奖（与无签名路径一致；第三方 SDK 场景无 bid，传空上下文）。
	bid := &BidContext{AppID: app.ID}
	s.fireRewardCallback(app, bid, "", req.UserID, time.Now().Unix())
	s.Log.Info("ad video-complete (signed)",
		"app", app.ID, "user_id", req.UserID, "adjust_adid", req.AdjustAdid)
	writeJSON(w, http.StatusOK, map[string]any{
		"ad_app_id":   req.AdAppID,
		"user_id":     req.UserID,
		"adjust_adid": req.AdjustAdid,
	})
}
