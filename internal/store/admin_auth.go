package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"

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
func (s *Store) GetAdminMenu(ctx context.Context, email string) ([]*MenuRow, error) {
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
	return out, rows.Err()
}
