package api

import (
	"net/http"
	"regexp"

	"adcenter/internal/store"
)

// ============================================================
// 后台 RBAC 管理 API：用户 / 角色 / 菜单（migration 000015/000016）
//
// 权限：全部接口 requireRole(needWrite=true, needSuper=true) —— 只有 super_admin
// 能动。改权限等同于改系统访问边界，必须收口到最高权限，且每次写操作落审计。
// ============================================================

// rbacCodeRe 角色/菜单的业务码：小写字母数字开头，允许 . _ -（前端路由与常量友好）。
var rbacCodeRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// ============================================================
// 用户
// ============================================================

// handleListUsers 后台用户列表。
//
//	GET /v1/admin/users
func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireRole(w, r, true, true); !ok {
		return
	}
	users, err := s.Store.ListUsers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, users)
}

// handleCreateUser 新增后台用户（按邮箱从 Supabase Auth 解析，邮箱未注册会明确报错）。
//
//	POST /v1/admin/users  {"email","role","status"?}
func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireRole(w, r, true, true)
	if !ok {
		return
	}
	var req struct {
		Email  string `json:"email"`
		Role   string `json:"role"`
		Status string `json:"status"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Email == "" || req.Role == "" {
		writeError(w, http.StatusBadRequest, "email and role required")
		return
	}
	if req.Status == "" {
		req.Status = "active"
	}
	if req.Status != "active" && req.Status != "disabled" {
		writeError(w, http.StatusBadRequest, "status must be active or disabled")
		return
	}
	id, err := s.Store.CreateUser(r.Context(), req.Email, req.Role, req.Status)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	_ = s.Store.WriteAudit(r.Context(), actor, "create", "admin_user", id, nil)
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

// handleUpdateUser 改角色 / 状态 / 邮箱（部分更新）。
//
//	PATCH /v1/admin/users/{id}
func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireRole(w, r, true, true)
	if !ok {
		return
	}
	var req struct {
		Email  *string `json:"email"`
		Role   *string `json:"role"`
		Status *string `json:"status"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	fields := map[string]any{}
	if req.Email != nil && *req.Email != "" {
		fields["email"] = *req.Email
	}
	if req.Role != nil && *req.Role != "" {
		fields["role"] = *req.Role
	}
	if req.Status != nil {
		if *req.Status != "active" && *req.Status != "disabled" {
			writeError(w, http.StatusBadRequest, "status must be active or disabled")
			return
		}
		fields["status"] = *req.Status
	}
	if len(fields) == 0 {
		writeError(w, http.StatusBadRequest, "no fields to update")
		return
	}
	id := r.PathValue("id")
	if err := s.Store.UpdateUser(r.Context(), id, fields); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	_ = s.Store.WriteAudit(r.Context(), actor, "update", "admin_user", id, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleDeleteUser 移除后台授权（不删 Supabase Auth 账号）。
//
//	DELETE /v1/admin/users/{id}
func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireRole(w, r, true, true)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if err := s.Store.DeleteUser(r.Context(), id); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	_ = s.Store.WriteAudit(r.Context(), actor, "delete", "admin_user", id, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ============================================================
// 角色
// ============================================================

// handleListRoles 角色列表（含已授权菜单数）。
//
//	GET /v1/admin/roles
func (s *Server) handleListRoles(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireRole(w, r, true, true); !ok {
		return
	}
	roles, err := s.Store.ListRoles(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, roles)
}

// handleCreateRole 新建自定义角色。
//
//	POST /v1/admin/roles  {"code","name","description"?}
func (s *Server) handleCreateRole(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireRole(w, r, true, true)
	if !ok {
		return
	}
	var req struct {
		Code        string `json:"code"`
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if !rbacCodeRe.MatchString(req.Code) || req.Name == "" {
		writeError(w, http.StatusBadRequest,
			"code must match ^[a-z0-9][a-z0-9._-]{0,63}$ and name required")
		return
	}
	if err := s.Store.CreateRole(r.Context(), req.Code, req.Name, req.Description); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	_ = s.Store.WriteAudit(r.Context(), actor, "create", "role", req.Code, nil)
	writeJSON(w, http.StatusCreated, map[string]string{"id": req.Code})
}

// handleUpdateRole 改角色名称 / 描述（code 不可改）。
//
//	PATCH /v1/admin/roles/{code}
func (s *Server) handleUpdateRole(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireRole(w, r, true, true)
	if !ok {
		return
	}
	var req struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	fields := map[string]any{}
	if req.Name != nil && *req.Name != "" {
		fields["name"] = *req.Name
	}
	if req.Description != nil {
		fields["description"] = *req.Description
	}
	if len(fields) == 0 {
		writeError(w, http.StatusBadRequest, "no fields to update")
		return
	}
	code := r.PathValue("code")
	if err := s.Store.UpdateRole(r.Context(), code, fields); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	_ = s.Store.WriteAudit(r.Context(), actor, "update", "role", code, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleDeleteRole 删除角色（内置角色不可删）。
//
//	DELETE /v1/admin/roles/{code}
func (s *Server) handleDeleteRole(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireRole(w, r, true, true)
	if !ok {
		return
	}
	code := r.PathValue("code")
	if err := s.Store.DeleteRole(r.Context(), code); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	_ = s.Store.WriteAudit(r.Context(), actor, "delete", "role", code, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleGetRoleMenus 某角色已授权的菜单 code（供后台勾选回显）。
//
//	GET /v1/admin/roles/{code}/menus
func (s *Server) handleGetRoleMenus(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireRole(w, r, true, true); !ok {
		return
	}
	codes, err := s.Store.RoleMenuCodes(r.Context(), r.PathValue("code"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string][]string{"menu_codes": codes})
}

// handleSetRoleMenus 全量替换角色授权（前端勾选后整份提交，与优先级同一语义）。
//
//	PUT /v1/admin/roles/{code}/menus  {"menu_codes":[...]}
func (s *Server) handleSetRoleMenus(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireRole(w, r, true, true)
	if !ok {
		return
	}
	var req struct {
		MenuCodes []string `json:"menu_codes"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	code := r.PathValue("code")
	if err := s.Store.SetRoleMenus(r.Context(), code, req.MenuCodes); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	_ = s.Store.WriteAudit(r.Context(), actor, "set_menus", "role", code, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ============================================================
// 菜单
// ============================================================

// handleListMenus 全部菜单（含停用）。
//
//	GET /v1/admin/menus
func (s *Server) handleListMenus(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireRole(w, r, true, true); !ok {
		return
	}
	menus, err := s.Store.ListMenus(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, menus)
}

// handleCreateMenu 新建菜单（href 为空 = 分组，parent_code 为空 = 顶层）。
//
//	POST /v1/admin/menus  {"code","label","href"?,"parent_code"?,"sort_order"?,"enabled"?}
func (s *Server) handleCreateMenu(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireRole(w, r, true, true)
	if !ok {
		return
	}
	var req struct {
		Code       string  `json:"code"`
		Label      string  `json:"label"`
		Href       string  `json:"href"`
		ParentCode string  `json:"parent_code"`
		SortOrder  int     `json:"sort_order"`
		Enabled    *bool   `json:"enabled"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if !rbacCodeRe.MatchString(req.Code) || req.Label == "" {
		writeError(w, http.StatusBadRequest,
			"code must match ^[a-z0-9][a-z0-9._-]{0,63}$ and label required")
		return
	}
	m := &store.AdminMenu{Code: req.Code, Label: req.Label, SortOrder: req.SortOrder, Enabled: true}
	if req.Enabled != nil {
		m.Enabled = *req.Enabled
	}
	if req.Href != "" {
		m.Href = &req.Href
	}
	if req.ParentCode != "" {
		m.ParentCode = &req.ParentCode
	}
	if err := s.Store.CreateMenu(r.Context(), m); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	_ = s.Store.WriteAudit(r.Context(), actor, "create", "menu", req.Code, nil)
	writeJSON(w, http.StatusCreated, map[string]string{"id": req.Code})
}

// handleUpdateMenu 改菜单标题 / 路由 / 父级 / 排序 / 启停（code 不可改）。
//
//	PATCH /v1/admin/menus/{code}
func (s *Server) handleUpdateMenu(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireRole(w, r, true, true)
	if !ok {
		return
	}
	var req struct {
		Label      *string `json:"label"`
		Href       *string `json:"href"`
		ParentCode *string `json:"parent_code"`
		SortOrder  *int    `json:"sort_order"`
		Enabled    *bool   `json:"enabled"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	fields := map[string]any{}
	if req.Label != nil && *req.Label != "" {
		fields["label"] = *req.Label
	}
	// 空串表示「该菜单是分组 / 挂在顶层」，落库为 NULL（而不是空字符串）
	if req.Href != nil {
		if *req.Href == "" {
			fields["href"] = nil
		} else {
			fields["href"] = *req.Href
		}
	}
	if req.ParentCode != nil {
		if *req.ParentCode == "" {
			fields["parent_code"] = nil
		} else {
			fields["parent_code"] = *req.ParentCode
		}
	}
	if req.SortOrder != nil {
		fields["sort_order"] = *req.SortOrder
	}
	if req.Enabled != nil {
		fields["enabled"] = *req.Enabled
	}
	if len(fields) == 0 {
		writeError(w, http.StatusBadRequest, "no fields to update")
		return
	}
	code := r.PathValue("code")
	if err := s.Store.UpdateMenu(r.Context(), code, fields); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	_ = s.Store.WriteAudit(r.Context(), actor, "update", "menu", code, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleDeleteMenu 删除菜单（授权关系与子菜单由外键级联清理）。
//
//	DELETE /v1/admin/menus/{code}
func (s *Server) handleDeleteMenu(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireRole(w, r, true, true)
	if !ok {
		return
	}
	code := r.PathValue("code")
	if err := s.Store.DeleteMenu(r.Context(), code); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	_ = s.Store.WriteAudit(r.Context(), actor, "delete", "menu", code, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
