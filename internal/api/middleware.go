package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"

	"adcenter/internal/config"
)

type ctxKey string

const (
	ctxApp  ctxKey = "app"
	ctxRole ctxKey = "role"
)

// authenticateApp 客户端 API 鉴权：X-Api-Key → sha256 → 快照查表（纯内存，
// 无 DB）。appID 由服务端推导，客户端不可自行声明（多租户隔离锚点）。
func (s *Server) authenticateApp(w http.ResponseWriter, r *http.Request) *config.App {
	key := r.Header.Get("X-Api-Key")
	if key == "" {
		writeError(w, http.StatusUnauthorized, "missing X-Api-Key")
		return nil
	}
	sum := sha256.Sum256([]byte(key))
	app := s.Cache.Snapshot().AppByKeyHash[hex.EncodeToString(sum[:])]
	if app == nil {
		writeError(w, http.StatusUnauthorized, "invalid api key")
		return nil
	}
	return app
}

// requireRole 管理 API RBAC：内部密钥校验 + actor 角色复核（Go 是权威执行点）。
// needWrite: true 表示写操作（operator/super_admin 可），false 只读（四角色皆可）。
// needSuper: true 表示仅 super_admin。
func (s *Server) requireRole(w http.ResponseWriter, r *http.Request, needWrite, needSuper bool) (email string, ok bool) {
	if s.InternalKey == "" {
		writeError(w, http.StatusInternalServerError, "internal key not configured")
		return "", false
	}
	if r.Header.Get("X-Internal-Key") != s.InternalKey {
		writeError(w, http.StatusUnauthorized, "invalid internal key")
		return "", false
	}
	email = r.Header.Get("X-Actor-Email")
	if email == "" {
		writeError(w, http.StatusUnauthorized, "missing actor")
		return "", false
	}
	role, err := s.Store.GetAdminRole(r.Context(), email)
	if err != nil {
		writeError(w, http.StatusForbidden, "actor has no role")
		return "", false
	}
	switch {
	case role == "super_admin":
		return email, true
	case needSuper:
		writeError(w, http.StatusForbidden, "super_admin required")
		return "", false
	case needWrite && (role == "operator" || role == "strategy"):
		return email, true
	case !needWrite:
		return email, true // analyst 只读放行
	default:
		writeError(w, http.StatusForbidden, "insufficient role: "+role)
		return "", false
	}
}

// writeJSON 统一 JSON 响应。
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError 统一错误响应。
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// decodeJSON 限制请求体大小的 JSON 解析。
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MB
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return false
	}
	return true
}
