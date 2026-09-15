package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// ============================================================
// 后台 RBAC：用户 / 角色 / 菜单（migration 000015）
//
// 此前权限硬编码在前端（角色 → 路由前缀），改一次权限要改代码发版。落到库里后
// 由后台自助维护：菜单决定侧边栏与可访问路由，角色决定能看到哪些菜单。
// 所有写接口的权限复核在 API 层（requireRole needSuper），此处只管数据。
// ============================================================

// AdminUser 后台用户（ads_center.admin_users；认证本体在 Supabase Auth）。
type AdminUser struct {
	AuthUserID string  `json:"id"`
	Email      string  `json:"email"`
	Role       string  `json:"role"`
	Status     string  `json:"status"` // active / disabled
	CreatedAt  *string `json:"created_at"`
}

// ListUsers 后台用户列表（按创建时间倒序）。
func (s *Store) ListUsers(ctx context.Context) ([]*AdminUser, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT code::text, email, role_code, status, created_at::text
		FROM admin_users
		ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*AdminUser
	for rows.Next() {
		u := &AdminUser{}
		if err := rows.Scan(&u.AuthUserID, &u.Email, &u.Role, &u.Status, &u.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// CreateUser 新增后台用户。
//
// id 不能凭空编造（登录走 Supabase Auth），因此按邮箱从 auth.users 解析；
// 该邮箱尚未在 Auth 注册时返回明确错误——需先让该用户登录一次才会出现。
func (s *Store) CreateUser(ctx context.Context, email, role, status string) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx, `
		INSERT INTO admin_users (code, email, role_code, status)
		SELECT u.id, $1, $2, $3 FROM auth.users u WHERE lower(u.email) = lower($1)
		RETURNING code::text`, email, role, status).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("邮箱 %s 尚未注册 Supabase Auth，请先让该用户登录一次", email)
	}
	if err != nil {
		return "", err
	}
	return id, nil
}

// userWritable 用户可写列（id 是主键，不可改）。
var userWritable = map[string]bool{"email": true, "role_code": true, "status": true}

// UpdateUser 更新用户的角色 / 状态 / 邮箱（部分更新）。
func (s *Store) UpdateUser(ctx context.Context, authUserID string, fields map[string]any) error {
	sets, args := buildUpdate(fields, userWritable, nil)
	if len(sets) == 0 {
		return fmt.Errorf("no writable fields")
	}
	args = append(args, authUserID)
	tag, err := s.pool.Exec(ctx, fmt.Sprintf(
		`UPDATE admin_users SET %s WHERE code = $%d`,
		strings.Join(sets, ", "), len(args)), args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("user not found: %s", authUserID)
	}
	return nil
}

// DeleteUser 删除后台用户（仅解除后台授权，不删 Supabase Auth 账号）。
func (s *Store) DeleteUser(ctx context.Context, authUserID string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM admin_users WHERE code = $1`, authUserID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("user not found: %s", authUserID)
	}
	return nil
}

// AdminRole 后台角色（含已授权菜单数，供列表展示）。
type AdminRole struct {
	Code        string  `json:"code"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	IsSystem    bool    `json:"is_system"`  // 内置角色：不可删除
	MenuCount   int     `json:"menu_count"` // 已授权菜单数
	CreatedAt   *string `json:"created_at"`
}

// ListRoles 角色列表（内置角色优先，其次按创建时间）。
func (s *Store) ListRoles(ctx context.Context) ([]*AdminRole, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT r.code, r.name, r.description, r.is_system,
		       (SELECT count(*) FROM role_menus rm WHERE rm.role_code = r.code),
		       r.created_at::text
		FROM roles r
		ORDER BY r.is_system DESC, r.created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*AdminRole
	for rows.Next() {
		ro := &AdminRole{}
		if err := rows.Scan(&ro.Code, &ro.Name, &ro.Description, &ro.IsSystem,
			&ro.MenuCount, &ro.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, ro)
	}
	return out, rows.Err()
}

// CreateRole 新建角色（code 为业务码，创建后不可改，避免授权关系断裂）。
func (s *Store) CreateRole(ctx context.Context, code, name, description string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO roles (code, name, description) VALUES ($1, $2, $3)`,
		code, name, description)
	return err
}

// roleWritable 角色可写列（code 是主键、is_system 是内置标记，均不可改）。
var roleWritable = map[string]bool{"name": true, "description": true}

// UpdateRole 更新角色名称 / 描述。
func (s *Store) UpdateRole(ctx context.Context, code string, fields map[string]any) error {
	sets, args := buildUpdate(fields, roleWritable, nil)
	if len(sets) == 0 {
		return fmt.Errorf("no writable fields")
	}
	args = append(args, code)
	tag, err := s.pool.Exec(ctx, fmt.Sprintf(
		`UPDATE roles SET %s WHERE code = $%d`, strings.Join(sets, ", "), len(args)), args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("role not found: %s", code)
	}
	return nil
}

// DeleteRole 删除角色（内置角色禁止删除，防止把超级管理员删掉导致后台不可用）。
func (s *Store) DeleteRole(ctx context.Context, code string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM roles WHERE code = $1 AND NOT is_system`, code)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("role not found or is system role: %s", code)
	}
	return nil
}

// AdminMenu 后台菜单（parent_code 非空表示挂在某个分组下；href 为 NULL 即分组）。
type AdminMenu struct {
	Code       string  `json:"code"`
	Label      string  `json:"label"`
	Href       *string `json:"href"`
	ParentCode *string `json:"parent_code"`
	SortOrder  int     `json:"sort_order"`
	Enabled    bool    `json:"enabled"`
}

// ListMenus 全部菜单（含停用，按排序与 code）。
func (s *Store) ListMenus(ctx context.Context) ([]*AdminMenu, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT code, label, href, parent_code, sort_order, enabled
		FROM menus
		ORDER BY sort_order, code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*AdminMenu
	for rows.Next() {
		m := &AdminMenu{}
		if err := rows.Scan(&m.Code, &m.Label, &m.Href, &m.ParentCode,
			&m.SortOrder, &m.Enabled); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// CreateMenu 新建菜单（href / parent_code 为 NULL 时表示这是一个分组）。
func (s *Store) CreateMenu(ctx context.Context, m *AdminMenu) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO menus (code, label, href, parent_code, sort_order, enabled)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		m.Code, m.Label, m.Href, m.ParentCode, m.SortOrder, m.Enabled)
	return err
}

// menuWritable 菜单可写列（code 是主键，不可改）。
var menuWritable = map[string]bool{
	"label": true, "href": true, "parent_code": true,
	"sort_order": true, "enabled": true,
}

// UpdateMenu 更新菜单。
func (s *Store) UpdateMenu(ctx context.Context, code string, fields map[string]any) error {
	sets, args := buildUpdate(fields, menuWritable, nil)
	if len(sets) == 0 {
		return fmt.Errorf("no writable fields")
	}
	args = append(args, code)
	tag, err := s.pool.Exec(ctx, fmt.Sprintf(
		`UPDATE menus SET %s WHERE code = $%d`, strings.Join(sets, ", "), len(args)), args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("menu not found: %s", code)
	}
	return nil
}

// DeleteMenu 删除菜单（role_menus 与子菜单由外键 ON DELETE CASCADE 连带清理）。
func (s *Store) DeleteMenu(ctx context.Context, code string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM menus WHERE code = $1`, code)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("menu not found: %s", code)
	}
	return nil
}

// SetRoleMenus 全量替换某角色的菜单授权（事务内先清后写，与填充优先级同一语义）。
func (s *Store) SetRoleMenus(ctx context.Context, roleCode string, menuCodes []string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) // 提交成功后再 Rollback 是 no-op
	if _, err := tx.Exec(ctx, `DELETE FROM role_menus WHERE role_code = $1`, roleCode); err != nil {
		return err
	}
	for _, mc := range menuCodes {
		if _, err := tx.Exec(ctx, `
			INSERT INTO role_menus (role_code, menu_code) VALUES ($1, $2)
			ON CONFLICT DO NOTHING`, roleCode, mc); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// RoleMenuCodes 某角色已授权的菜单 code（供后台勾选回显）。
func (s *Store) RoleMenuCodes(ctx context.Context, roleCode string) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT menu_code FROM role_menus WHERE role_code = $1 ORDER BY menu_code`, roleCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, err
		}
		out = append(out, code)
	}
	return out, rows.Err()
}
