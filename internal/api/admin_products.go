package api

import (
	"net/http"

	"adcenter/internal/store"
)

// ============================================================
// 产品（Product）CRUD（管理 API，BFF 转发）
// 产品挂在广告主下，用于多产品广告主的归集。
// ============================================================

var productStatuses = map[string]bool{"active": true, "paused": true}

// productRequest 产品创建/更新请求体。
type productRequest struct {
	AdvertiserID string  `json:"advertiser_id"`
	Name         string  `json:"name"`
	Status       string  `json:"status"`
	DailyBudget  float64 `json:"daily_budget"`
	Notes        string  `json:"notes"`
}

// validate 校验产品请求。
func (r productRequest) validate() error {
	if r.AdvertiserID == "" || r.Name == "" {
		return errMissing("advertiser_id, name")
	}
	if r.Status != "" && !productStatuses[r.Status] {
		return errInvalid("status")
	}
	return nil
}

func (r productRequest) fields() map[string]any {
	f := map[string]any{
		"advertiser_id": r.AdvertiserID,
		"name":          r.Name,
		"status":        orDefault(r.Status, "active"),
		"daily_budget":  r.DailyBudget,
		"notes":         r.Notes,
	}
	return f
}

func (r productRequest) updateFields() map[string]any {
	f := map[string]any{}
	if r.Name != "" {
		f["name"] = r.Name
	}
	if r.Status != "" {
		f["status"] = r.Status
	}
	if r.DailyBudget != 0 {
		f["daily_budget"] = r.DailyBudget
	}
	if r.Notes != "" {
		f["notes"] = r.Notes
	}
	return f
}

// handleListProducts 产品列表（?advertiser_id= 可选过滤）。
func (s *Server) handleListProducts(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireRole(w, r, false, false); !ok {
		return
	}
	advertiserID := r.URL.Query().Get("advertiser_id")
	list, err := s.Store.ListProducts(r.Context(), advertiserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if list == nil {
		list = []*store.AdminProduct{}
	}
	writeJSON(w, http.StatusOK, list)
}

// handleCreateProduct 新建产品。
func (s *Server) handleCreateProduct(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireRole(w, r, true, false)
	if !ok {
		return
	}
	var req productRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := req.validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	fields := req.fields()
	fields["created_by"] = actor
	fields["updated_by"] = actor
	id, err := s.Store.CreateProduct(r.Context(), fields)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	_ = s.Store.WriteAudit(r.Context(), actor, "create", "product", id, nil)
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

// handleGetProduct 产品详情。
func (s *Server) handleGetProduct(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireRole(w, r, false, false); !ok {
		return
	}
	d, err := s.Store.GetProduct(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// handleUpdateProduct 更新产品。
func (s *Server) handleUpdateProduct(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireRole(w, r, true, false)
	if !ok {
		return
	}
	id := r.PathValue("id")
	var req productRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	fields := req.updateFields()
	if len(fields) == 0 {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	fields["updated_by"] = actor
	if err := s.Store.UpdateProduct(r.Context(), id, fields); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	_ = s.Store.WriteAudit(r.Context(), actor, "update", "product", id, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleDeleteProduct 软删产品。
func (s *Server) handleDeleteProduct(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireRole(w, r, true, false)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if err := s.Store.SoftDeleteProduct(r.Context(), id); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	_ = s.Store.WriteAudit(r.Context(), actor, "delete", "product", id, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
