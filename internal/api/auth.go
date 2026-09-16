package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"

	"adcenter/internal/store"
)

// ============================================================
// 后台自管登录（migration 000044）：不再依赖 Supabase Auth。
//
// 流程：
//   POST /v1/admin/login  {email,password}
//     → 后端验密（store.VerifyPassword）→ 签发 HMAC 会话令牌（email|role|exp）
//     → 返回 {token, role, menu}
//   前端把 token 存 httpOnly cookie，后续请求经 BFF 验签后转发 X-Actor-Email。
//   GET  /v1/admin/me  → 凭 Bearer token 返回 {email, role, menu}（供前端拿身份与菜单）
//
// 令牌用 SESSION_SECRET 做 HMAC-SHA256，纯标准库实现，零新依赖。
// ============================================================

const sessionTTL = 7 * 24 * time.Hour

// handleLogin 后台登录：开放端点（无内部密钥），是获取会话的唯一入口。
//
//	POST /v1/admin/login  {"email","password"}
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if s.SessionSecret == "" {
		writeError(w, http.StatusInternalServerError, "session secret not configured")
		return
	}
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Email == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "email and password required")
		return
	}

	role, status, ph, err := s.Store.AuthenticateAdmin(r.Context(), req.Email)
	if err != nil || ph == "" {
		// 账号不存在 / 未设密码 统一报 401，避免用户枚举
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if status != "active" {
		writeError(w, http.StatusForbidden, "account disabled")
		return
	}
	if !store.VerifyPassword(ph, req.Password) {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	token, err := s.signSession(req.Email, role)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "sign failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"token": token,
		"role":  role,
		"menu":  s.menuRows(r, req.Email),
	})
}

// handleAdminMe 当前登录用户信息 + 菜单（凭 Bearer token，供前端拿身份与侧边栏）。
//
//	GET /v1/admin/me
func (s *Server) handleAdminMe(w http.ResponseWriter, r *http.Request) {
	if s.SessionSecret == "" {
		writeError(w, http.StatusInternalServerError, "session secret not configured")
		return
	}
	email, role, ok := s.sessionFromHeader(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"email": email,
		"role":  role,
		"menu":  s.menuRows(r, email),
	})
}

// menuRows 把存储层菜单行原样转成前端 buildTree 期望的 menu_* 结构。
func (s *Server) menuRows(r *http.Request, email string) []*store.MenuRow {
	menu, err := s.Store.GetAdminMenu(r.Context(), email)
	if err != nil || len(menu) == 0 {
		return []*store.MenuRow{}
	}
	return menu
}

// signSession 签发 HMAC 会话令牌：base64(email|role|exp).hex(hmac)。
func (s *Server) signSession(email, role string) (string, error) {
	payload := email + "|" + role + "|" + strconv.FormatInt(time.Now().Add(sessionTTL).Unix(), 10)
	raw := []byte(payload)
	mac := hmac.New(sha256.New, []byte(s.SessionSecret))
	mac.Write(raw)
	sig := hex.EncodeToString(mac.Sum(nil))
	return base64.URLEncoding.EncodeToString(raw) + "." + sig, nil
}

// sessionFromHeader 从 Authorization: Bearer <token> 解出 email/role。
func (s *Server) sessionFromHeader(r *http.Request) (email, role string, ok bool) {
	const prefix = "Bearer "
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, prefix) {
		return "", "", false
	}
	return s.verifySession(strings.TrimPrefix(auth, prefix))
}

// verifySession 校验令牌签名与有效期，返回 email/role。
func (s *Server) verifySession(token string) (email, role string, ok bool) {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	raw, err := base64.URLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", "", false
	}
	mac := hmac.New(sha256.New, []byte(s.SessionSecret))
	mac.Write(raw)
	want := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(want), []byte(parts[1])) {
		return "", "", false
	}
	segs := strings.Split(string(raw), "|")
	if len(segs) != 3 {
		return "", "", false
	}
	exp, err := strconv.ParseInt(segs[2], 10, 64)
	if err != nil {
		return "", "", false
	}
	if time.Now().Unix() > exp {
		return "", "", false
	}
	return segs[0], segs[1], true
}
