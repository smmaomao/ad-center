package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

// ============================================================
// 后台自管鉴权（migration 000044）：密码哈希 + 按邮箱取角色/菜单。
//
// 此前身份完全依赖 Supabase Auth（auth.users），current_admin_role /
// current_admin_menus 两个 RPC 用 auth.uid() 取当前用户。迁移到后端自管后：
//   - 密码存 password_hash（sha256 + 随机盐 + 高轮次迭代，纯标准库，零新依赖）
//   - 角色 / 菜单直接按邮箱查，不再依赖 Supabase 会话上下文
// ============================================================

const pwIterations = 100000

// HashPassword 生成 sha256$<saltHex>$<hashHex>，hash 为 salt+password 迭代 pwIterations 次。
func HashPassword(pw string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	h := sha256.Sum256(append(append([]byte{}, salt...), []byte(pw)...))
	for i := 1; i < pwIterations; i++ {
		h = sha256.Sum256(h[:])
	}
	return "sha256$" + hex.EncodeToString(salt) + "$" + hex.EncodeToString(h[:]), nil
}

// VerifyPassword 校验明文密码与存储哈希是否匹配（恒定时间比较）。
func VerifyPassword(stored, pw string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) != 3 || parts[0] != "sha256" {
		return false
	}
	salt, err := hex.DecodeString(parts[1])
	if err != nil {
		return false
	}
	want, err := hex.DecodeString(parts[2])
	if err != nil {
		return false
	}
	h := sha256.Sum256(append(append([]byte{}, salt...), []byte(pw)...))
	for i := 1; i < pwIterations; i++ {
		h = sha256.Sum256(h[:])
	}
	// 长度不一致时 Equal 返回 false，安全
	if len(h) != len(want) {
		return false
	}
	var diff byte
	for i := range h {
		diff |= h[i] ^ want[i]
	}
	return diff == 0
}

// AuthenticateAdmin 按邮箱取角色 / 状态 / 密码哈希。账号不存在返回错误（上层统一报 401）。
func (s *Store) AuthenticateAdmin(ctx context.Context, email string) (role, status, passwordHash string, err error) {
	var ph sql.NullString
	err = s.pool.QueryRow(ctx, `
		SELECT role_code, status, password_hash
		FROM admin_users
		WHERE lower(email) = lower($1)`, email).
		Scan(&role, &status, &ph)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", "", errors.New("no such user")
	}
	if err != nil {
		return "", "", "", err
	}
	if ph.Valid {
		passwordHash = ph.String
	}
	return role, status, passwordHash, nil
}

// MenuRow 后台菜单行（JSON 字段名与前端 buildTree 期望的 menu_* 对齐）。
type MenuRow struct {
	Code       string  `json:"menu_code"`
	Label      string  `json:"menu_label"`
	Href       *string `json:"menu_href"`
	ParentCode *string `json:"menu_parent_code"`
	SortOrder  int     `json:"menu_sort_order"`
}

// GetAdminMenu 当前用户（按邮箱）可见菜单树 —— 替代 current_admin_menus RPC。
// 返回被授权菜单 + 它们的父链（保证侧边栏分组框架不丢），enabled 才展示。
//
// 结果走进程内短缓存（见文件末尾「RBAC 查询短缓存」）：后台每次切菜单都要
// 重查这棵递归树，而授权极少变动，没必要每次打库。
func (s *Store) GetAdminMenu(ctx context.Context, email string) ([]*MenuRow, error) {
	key := adminAuthKey(email)
	if rows, ok := s.adminAuth.getMenu(key); ok {
		return rows, nil
	}
	rows, err := s.pool.Query(ctx, `
		WITH RECURSIVE granted AS (
			SELECT rm.menu_code
			FROM ads_center.role_menus rm
					 JOIN ads_center.admin_users u ON u.role_code = rm.role_code
			WHERE lower(u.email) = lower($1)
			  AND u.status = 'active'
		), tree AS (
			SELECT m.code, m.label, m.href, m.parent_code, m.sort_order
			FROM ads_center.menus m
			WHERE m.enabled
			  AND m.code IN (SELECT menu_code FROM granted)
			UNION
			SELECT m.code, m.label, m.href, m.parent_code, m.sort_order
			FROM ads_center.menus m
					 JOIN tree t ON t.parent_code = m.code
			WHERE m.enabled
		)
		SELECT DISTINCT code, label, href, parent_code, sort_order
		FROM tree
		ORDER BY parent_code NULLS FIRST, sort_order, code`, email)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*MenuRow
	for rows.Next() {
		m := &MenuRow{}
		if err := rows.Scan(&m.Code, &m.Label, &m.Href, &m.ParentCode, &m.SortOrder); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	s.adminAuth.putMenu(key, out)
	return out, nil
}

// ============================================================
// RBAC 查询短缓存
//
// 后台每切一次菜单（一次页面导航）都要打库 3 次：
//   1. /v1/admin/me → GetAdminMenu（WITH RECURSIVE 菜单树）
//   2. 列表接口 → GetAdminRole（RBAC 校验，每个后台接口都要过）
//   3. 列表接口 → 真正的业务数据
// 前两者查的是极少变动的授权配置，每次导航重查纯属浪费。这里给它们加进程内
// 短 TTL 缓存，把每次导航的查库次数从 3 降到 1（只剩业务数据那一次）。
//
// 一致性：后台所有 RBAC 写操作（用户 / 角色 / 菜单 / 角色授权）都会调用
// InvalidateAdminAuth 立刻清空，所以通过后台改的权限即时生效；多实例部署时，
// 处理写请求的实例立刻失效，其余实例最多残留 adminAuthTTL 后自动过期——
// 授权变更的可接受窗口（不引入跨实例广播的复杂度）。
// ============================================================

const adminAuthTTL = 15 * time.Second

type adminAuthEntry[V any] struct {
	val     V
	expires time.Time
}

type adminAuthCache struct {
	mu   sync.RWMutex
	role map[string]adminAuthEntry[string]
	menu map[string]adminAuthEntry[[]*MenuRow]
}

func newAdminAuthCache() *adminAuthCache {
	return &adminAuthCache{
		role: make(map[string]adminAuthEntry[string]),
		menu: make(map[string]adminAuthEntry[[]*MenuRow]),
	}
}

// adminAuthKey 归一化邮箱：两处查询都用 lower(email) 匹配，缓存键保持一致。
func adminAuthKey(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func (c *adminAuthCache) getRole(key string) (string, bool) {
	if c == nil || key == "" {
		return "", false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.role[key]
	if !ok || time.Now().After(e.expires) {
		return "", false
	}
	return e.val, true
}

func (c *adminAuthCache) putRole(key, role string) {
	if c == nil || key == "" {
		return
	}
	c.mu.Lock()
	c.role[key] = adminAuthEntry[string]{val: role, expires: time.Now().Add(adminAuthTTL)}
	c.mu.Unlock()
}

// getMenu 返回副本，避免调用方改动缓存内的切片。
func (c *adminAuthCache) getMenu(key string) ([]*MenuRow, bool) {
	if c == nil || key == "" {
		return nil, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.menu[key]
	if !ok || time.Now().After(e.expires) {
		return nil, false
	}
	out := make([]*MenuRow, len(e.val))
	copy(out, e.val)
	return out, true
}

func (c *adminAuthCache) putMenu(key string, rows []*MenuRow) {
	if c == nil || key == "" {
		return
	}
	cp := make([]*MenuRow, len(rows))
	copy(cp, rows)
	c.mu.Lock()
	c.menu[key] = adminAuthEntry[[]*MenuRow]{val: cp, expires: time.Now().Add(adminAuthTTL)}
	c.mu.Unlock()
}

// invalidate 全清：授权变更极少见，整体清空比逐条失效更不容易出错。
func (c *adminAuthCache) invalidate() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.role = make(map[string]adminAuthEntry[string])
	c.menu = make(map[string]adminAuthEntry[[]*MenuRow])
	c.mu.Unlock()
}

// InvalidateAdminAuth 授权变更后清空 RBAC 缓存（用户 / 角色 / 菜单 / 授权写操作调用）。
func (s *Store) InvalidateAdminAuth() {
	s.adminAuth.invalidate()
}
