package api

import (
	"fmt"
	"net/http"

	"adcenter/internal/store"
)

// ============================================================
// 广告任务（Campaign）CRUD（管理 API，BFF 转发）
// campaign 是 KPI / 预算的执行粒度：出价 / 目标 CPI / 日预算 / 保量 / 消耗节奏
// 都落在 campaign 上，产品与广告主只做归集层。
// ============================================================

var campaignStatuses = map[string]bool{"active": true, "paused": true}
var campaignBillingModes = map[string]bool{
	"cpm": true, "cpc": true, "cpi": true,
	"cpa-activate": true, "cpa-register": true,
	"cpa-first-deposit": true, "cpa-pay": true,
}

// campaignRequest 广告任务创建/更新请求体。
type campaignRequest struct {
	AdvertiserID        string                `json:"advertiser_id"`
	Name                string                `json:"name"`
	Status              string                `json:"status"`
	BiddingPrice        float64               `json:"bidding_price"`
	BiddingPriceMin     float64               `json:"bidding_price_min"`
	BillingMode         string                `json:"billing_mode"`
	CPAEventPrices      map[string][2]float64 `json:"cpa_event_prices"`
	TargetKPIType       string                `json:"target_kpi_type"`
	TargetKPIValue      float64               `json:"target_kpi_value"`
	DailyBudget         float64               `json:"daily_budget"`
	ConsumeSpeed        int                   `json:"consume_speed"`
	GuaranteedEnabled   bool                  `json:"guaranteed_enabled"`
	GuaranteedMinShare  float64               `json:"guaranteed_min_share"`
	CreativeIDs         []string              `json:"creative_ids"`
	ProductID           string                `json:"product_id"`
	FreqDailyLimit      int                   `json:"freq_daily_limit"`
	FreqIntervalMinutes int                   `json:"freq_interval_minutes"`
	FreqFatigueWindow   int                   `json:"freq_fatigue_window"`
	StartAt             *string               `json:"start_at"`
	EndAt               *string               `json:"end_at"`
	DeliverTTLMinutes   int                   `json:"deliver_ttl_minutes"`
}

// validateCampaign 校验并打印可用的枚举默认值；返回用于落库的字段。
func (r campaignRequest) validate() error {
	if r.Name == "" {
		return errMissing("name")
	}
	if r.AdvertiserID == "" && r.ProductID == "" {
		return errMissing("advertiser_id or product_id")
	}
	if r.Status != "" && !campaignStatuses[r.Status] {
		return errInvalid("status")
	}
	if r.BillingMode != "" && !campaignBillingModes[r.BillingMode] {
		return errInvalid("billing_mode")
	}
	if r.TargetKPIType != "" && !campaignBillingModes[r.TargetKPIType] {
		return errInvalid("target_kpi_type")
	}
	if r.ConsumeSpeed != 0 && (r.ConsumeSpeed < 1 || r.ConsumeSpeed > 10) {
		return errInvalid("consume_speed (1-10)")
	}
	return nil
}

func (r campaignRequest) fields() map[string]any {
	f := map[string]any{
		"advertiser_id":        r.AdvertiserID,
		"name":                 r.Name,
		"bidding_price":        r.BiddingPrice,
		"bidding_price_min":    r.BiddingPriceMin,
		"billing_mode":         orDefault(r.BillingMode, "cpm"),
		"target_kpi_type":      r.TargetKPIType,
		"target_kpi_value":     r.TargetKPIValue,
		"daily_budget":         r.DailyBudget,
		"consume_speed":        orDefaultInt(r.ConsumeSpeed, 5),
		"deliver_ttl_minutes":  r.DeliverTTLMinutes,
		"guaranteed_enabled":   r.GuaranteedEnabled,
		"guaranteed_min_share": r.GuaranteedMinShare,
		"creative_ids":         r.CreativeIDs,
		"status":               orDefault(r.Status, "active"),
	}
	if r.CPAEventPrices != nil {
		f["cpa_event_prices"] = r.CPAEventPrices
	}
	if r.ProductID != "" {
		f["product_id"] = r.ProductID
	}
	if r.StartAt != nil {
		f["start_at"] = *r.StartAt
	}
	if r.EndAt != nil {
		f["end_at"] = *r.EndAt
	}
	return f
}

func (r campaignRequest) updateFields() map[string]any {
	f := map[string]any{}
	if r.Name != "" {
		f["name"] = r.Name
	}
	if r.Status != "" {
		f["status"] = r.Status
	}
	if r.BiddingPrice != 0 {
		f["bidding_price"] = r.BiddingPrice
	}
	if r.BiddingPriceMin != 0 {
		f["bidding_price_min"] = r.BiddingPriceMin
	}
	if r.BillingMode != "" {
		f["billing_mode"] = r.BillingMode
	}
	if r.CPAEventPrices != nil {
		f["cpa_event_prices"] = r.CPAEventPrices
	}
	if r.TargetKPIType != "" {
		f["target_kpi_type"] = r.TargetKPIType
	}
	if r.TargetKPIValue != 0 {
		f["target_kpi_value"] = r.TargetKPIValue
	}
	if r.DailyBudget != 0 {
		f["daily_budget"] = r.DailyBudget
	}
	if r.ConsumeSpeed != 0 {
		f["consume_speed"] = r.ConsumeSpeed
	}
	if r.DeliverTTLMinutes != 0 {
		f["deliver_ttl_minutes"] = r.DeliverTTLMinutes
	}
	f["guaranteed_enabled"] = r.GuaranteedEnabled
	if r.GuaranteedMinShare != 0 {
		f["guaranteed_min_share"] = r.GuaranteedMinShare
	}
	if r.FreqDailyLimit != 0 {
		f["freq_daily_limit"] = r.FreqDailyLimit
	}
	if r.FreqIntervalMinutes != 0 {
		f["freq_interval_minutes"] = r.FreqIntervalMinutes
	}
	if r.FreqFatigueWindow != 0 {
		f["freq_fatigue_window"] = r.FreqFatigueWindow
	}
	if r.CreativeIDs != nil {
		f["creative_ids"] = r.CreativeIDs
	}
	if r.ProductID != "" {
		f["product_id"] = r.ProductID
	}
	if r.StartAt != nil {
		f["start_at"] = *r.StartAt
	}
	if r.EndAt != nil {
		f["end_at"] = *r.EndAt
	}
	return f
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func orDefaultInt(v, def int) int {
	if v == 0 {
		return def
	}
	return v
}

// handleListCampaigns 广告任务列表。
func (s *Server) handleListCampaigns(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireRole(w, r, false, false); !ok {
		return
	}
	list, err := s.Store.ListCampaigns(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if list == nil {
		list = []*store.AdminCampaign{}
	}
	writeJSON(w, http.StatusOK, list)
}

// handleCreateCampaign 新建广告任务。
func (s *Server) handleCreateCampaign(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireRole(w, r, true, false)
	if !ok {
		return
	}
	var req campaignRequest
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
	id, err := s.Store.CreateCampaign(r.Context(), fields)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	_ = s.Store.WriteAudit(r.Context(), actor, "create", "campaign", id, nil)
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

// handleGetCampaign 广告任务详情。
func (s *Server) handleGetCampaign(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireRole(w, r, false, false); !ok {
		return
	}
	d, err := s.Store.GetCampaign(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// handleUpdateCampaign 更新广告任务。
func (s *Server) handleUpdateCampaign(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireRole(w, r, true, false)
	if !ok {
		return
	}
	id := r.PathValue("id")
	var req campaignRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	fields := req.updateFields()
	if len(fields) == 0 {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	fields["updated_by"] = actor
	if err := s.Store.UpdateCampaign(r.Context(), id, fields); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	_ = s.Store.WriteAudit(r.Context(), actor, "update", "campaign", id, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleDeleteCampaign 软删广告任务。
func (s *Server) handleDeleteCampaign(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireRole(w, r, true, false)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if err := s.Store.SoftDeleteCampaign(r.Context(), id); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	_ = s.Store.WriteAudit(r.Context(), actor, "delete", "campaign", id, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleRollupAdvertiserKPIs 广告主维度 KPI 汇总（旗下所有 campaign）。
func (s *Server) handleRollupAdvertiserKPIs(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireRole(w, r, false, false); !ok {
		return
	}
	roll, err := s.Store.RollupAdvertiserKPIs(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, roll)
}

// handleRollupProductKPIs 产品维度 KPI 汇总（该产品下所有 campaign）。
func (s *Server) handleRollupProductKPIs(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireRole(w, r, false, false); !ok {
		return
	}
	roll, err := s.Store.RollupProductKPIs(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, roll)
}

// 校验辅助（复用 error 文案）。
func errMissing(fields string) error {
	return fmt.Errorf("%s required", fields)
}
func errInvalid(field string) error {
	return fmt.Errorf("invalid %s", field)
}
