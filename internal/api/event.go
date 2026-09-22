package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"adcenter/internal/store"
)

// enqueueEvent 决策/回执路径的唯一写入口：只入队，绝不触碰 DB。
func (s *Server) enqueueEvent(e store.AdEvent) {
	if s.Queue == nil {
		return
	}
	s.Queue.Publish(e)
}

// ConsumeEvents 消费端批量落库（M2，SCALING.md §2）。
//
// 明细写 ad_events + 扣费金额按「广告主 × 小时」聚合写 budget_ledger，
// 两步在**同一事务**内：消费是至少一次语义，处理失败会重投，分步写会出现
// "明细写了、流水没写"的半截状态。
//
// 返回 error 时 Redis 后端不 ACK（消息留在 PEL 稍后重投）；
// Memory 后端无重试，由对账修正。
func (s *Server) ConsumeEvents(ctx context.Context, batch []store.AdEvent) error {
	if s.Store == nil {
		return nil
	}
	return s.Store.WriteEventBatch(ctx, batch)
}

// ============================================================
// 管理 API handlers
// ============================================================

func (s *Server) handleListAdvertisers(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireRole(w, r, false, false); !ok {
		return
	}
	list, err := s.Store.ListAdvertisers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if list == nil {
		list = []*store.AdminAdvertiser{}
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleGetAdvertiser(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireRole(w, r, false, false); !ok {
		return
	}
	a, err := s.Store.GetAdvertiser(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) handleCreateAdvertiser(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireRole(w, r, true, false)
	if !ok {
		return
	}
	var fields map[string]any
	if !decodeJSON(w, r, &fields) {
		return
	}
	fields["updated_by"] = actor
	id, err := s.Store.CreateAdvertiser(r.Context(), fields)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	_ = s.Store.WriteAudit(r.Context(), actor, "create", "advertiser", id, nil)
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (s *Server) handleUpdateAdvertiser(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireRole(w, r, true, false)
	if !ok {
		return
	}
	id := r.PathValue("id")
	var fields map[string]any
	if !decodeJSON(w, r, &fields) {
		return
	}
	fields["updated_by"] = actor
	if err := s.Store.UpdateAdvertiser(r.Context(), id, fields); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	_ = s.Store.WriteAudit(r.Context(), actor, "update", "advertiser", id, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleDeleteAdvertiser(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireRole(w, r, true, true) // 仅 super_admin
	if !ok {
		return
	}
	id := r.PathValue("id")
	if err := s.Store.SoftDeleteAdvertiser(r.Context(), id); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	_ = s.Store.WriteAudit(r.Context(), actor, "delete", "advertiser", id, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ---- 广告主总钱包（充值 - 扣费）----

// handleGetWallet 钱包概览：启用状态 / 当前余额 / 累计充值 / 累计扣费。
func (s *Server) handleGetWallet(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireRole(w, r, false, false); !ok {
		return
	}
	wl, err := s.Store.GetWallet(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, wl)
}

// handleListWalletFlow 钱包流水（充值 + 扣费合并，时间倒序）。
func (s *Server) handleListWalletFlow(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireRole(w, r, false, false); !ok {
		return
	}
	limit := 200
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	list, err := s.Store.ListWalletFlow(r.Context(), r.PathValue("id"), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if list == nil {
		list = []*store.WalletFlowRow{}
	}
	writeJSON(w, http.StatusOK, list)
}

// handleRecharge 广告主充值：写充值流水 + 递增余额 + 启用钱包闸，并即时更新
// 进程内闸门（不等下一轮对账）。
//
//	POST /v1/admin/advertisers/{id}/wallet/recharge  {"amount":1000,"currency":"USD","note":""}
func (s *Server) handleRecharge(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireRole(w, r, true, false)
	if !ok {
		return
	}
	var body struct {
		Amount   float64 `json:"amount"`
		Currency string  `json:"currency"`
		Note     string  `json:"note"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Amount <= 0 {
		writeError(w, http.StatusBadRequest, "amount must be positive")
		return
	}
	id := r.PathValue("id")
	balance, err := s.Store.Recharge(r.Context(), id, body.Amount, body.Currency, body.Note, actor)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if s.Budget != nil {
		s.Budget.WalletCredit(id, body.Amount)
	}
	_ = s.Store.WriteAudit(r.Context(), actor, "recharge", "advertiser", id, nil)
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "balance": balance})
}

// handleAdjustWallet 广告主钱包手动调账：修正余额 + 写调账流水（不改钱包闸开关）。
// 余额变更落在 advertisers 表，NOTIFY/对账会自动把进程内实时闸对齐。
//
//	POST /v1/admin/advertisers/{id}/wallet/adjust
//	  {"mode":"set","amount":500,"note":"冲正"}   // 把余额改为 500
//	  {"mode":"delta","amount":-50,"note":"补差"} // 余额减 50
func (s *Server) handleAdjustWallet(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireRole(w, r, true, false)
	if !ok {
		return
	}
	var body struct {
		Mode   string  `json:"mode"`
		Amount float64 `json:"amount"`
		Note   string  `json:"note"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Mode != "set" && body.Mode != "delta" {
		writeError(w, http.StatusBadRequest, "mode must be set or delta")
		return
	}
	id := r.PathValue("id")
	balance, err := s.Store.AdjustWallet(r.Context(), id, store.WalletAdjustInput{
		Mode:   body.Mode,
		Amount: body.Amount,
		Note:   body.Note,
	}, actor)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	_ = s.Store.WriteAudit(r.Context(), actor, "wallet_adjust", "advertiser", id, nil)
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "balance": balance})
}

func (s *Server) handleListSlots(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireRole(w, r, false, false); !ok {
		return
	}
	list, err := s.Store.ListSlots(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if list == nil {
		list = []*store.AdminSlot{}
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleListApps(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireRole(w, r, false, false); !ok {
		return
	}
	list, err := s.Store.ListApps(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if list == nil {
		list = []*store.AdminApp{}
	}
	writeJSON(w, http.StatusOK, list)
}

// handleCreateApp 注册 App：生成 API Key，明文与哈希一并落库（明文仅作展示/复制）。
func (s *Server) handleCreateApp(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireRole(w, r, true, true) // 仅 super_admin
	if !ok {
		return
	}
	var body struct {
		Name          string `json:"name"`
		AdWatchParams string `json:"ad_watch_params"`
		AddServerID   string `json:"add_server_id"`
	}
	if !decodeJSON(w, r, &body) || body.Name == "" {
		writeError(w, http.StatusBadRequest, "name required")
		return
	}
	if body.AdWatchParams != "" {
		var tmp map[string]any
		if err := json.Unmarshal([]byte(body.AdWatchParams), &tmp); err != nil {
			writeError(w, http.StatusBadRequest, "ad_watch_params must be a JSON object")
			return
		}
	}
	key, hash, err := generateAPIKey()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	id, err := s.Store.CreateApp(r.Context(), body.Name, key, hash, body.AdWatchParams, body.AddServerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.Store.WriteAudit(r.Context(), actor, "create", "app", id, nil)
	// 完整 Key 仅在创建时返回一次；之后页面常驻展示脱敏前缀，可复制完整值。
	writeJSON(w, http.StatusCreated, map[string]string{"id": id, "api_key": key})
}

// handleUpdateApp 更新 App 名称 / 状态 / 业务后端回调地址 / 完播回传附加参数 / app 服务端 id。
//
//	PATCH /v1/admin/apps/{id}  {"name"?,"status"?,"callback_url"?,"ad_watch_params"?,"add_server_id"?}
func (s *Server) handleUpdateApp(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireRole(w, r, true, true) // 仅 super_admin
	if !ok {
		return
	}
	var body struct {
		Name          *string `json:"name"`
		Status        *string `json:"status"`
		CallbackURL   *string `json:"callback_url"`
		AdWatchParams *string `json:"ad_watch_params"`
		AddServerID   *string `json:"add_server_id"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	fields := map[string]any{}
	if body.Name != nil && *body.Name != "" {
		fields["name"] = *body.Name
	}
	if body.Status != nil {
		if *body.Status != "active" && *body.Status != "paused" {
			writeError(w, http.StatusBadRequest, "status must be active or paused")
			return
		}
		fields["status"] = *body.Status
	}
	if body.CallbackURL != nil {
		fields["callback_url"] = *body.CallbackURL
	}
	if body.AdWatchParams != nil {
		if *body.AdWatchParams != "" {
			var tmp map[string]any
			if err := json.Unmarshal([]byte(*body.AdWatchParams), &tmp); err != nil {
				writeError(w, http.StatusBadRequest, "ad_watch_params must be a JSON object")
				return
			}
		}
		fields["ad_watch_params"] = *body.AdWatchParams
	}
	if body.AddServerID != nil {
		fields["add_server_id"] = *body.AddServerID
	}
	if len(fields) == 0 {
		writeError(w, http.StatusBadRequest, "no fields to update")
		return
	}
	id := r.PathValue("id")
	if err := s.Store.UpdateApp(r.Context(), id, fields); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	_ = s.Store.WriteAudit(r.Context(), actor, "update", "app", id, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleDeleteApp 软删 App：仅置 deleted_at，保留历史归因与事件数据。
//
//	DELETE /v1/admin/apps/{id}
func (s *Server) handleDeleteApp(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireRole(w, r, true, true) // 仅 super_admin
	if !ok {
		return
	}
	id := r.PathValue("id")
	if err := s.Store.SoftDeleteApp(r.Context(), id); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	_ = s.Store.WriteAudit(r.Context(), actor, "delete", "app", id, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleResetAppSecret 重置 S2S 签名密钥：旧密钥立即失效，业务后端需同步更新。
//
//	POST /v1/admin/apps/{id}/secret
func (s *Server) handleResetAppSecret(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireRole(w, r, true, true) // 仅 super_admin
	if !ok {
		return
	}
	id := r.PathValue("id")
	secret, err := s.Store.ResetAppSecret(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	_ = s.Store.WriteAudit(r.Context(), actor, "reset_secret", "app", id, nil)
	writeJSON(w, http.StatusOK, map[string]string{"secret": secret})
}

// handleResetAppKey 重置 API Key：旧密钥立即失效，新密钥明文仅在本次返回一次。
//
//	POST /v1/admin/apps/{id}/key
func (s *Server) handleResetAppKey(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireRole(w, r, true, true) // 仅 super_admin
	if !ok {
		return
	}
	id := r.PathValue("id")
	key, hash, err := generateAPIKey()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := s.Store.ResetAppKey(r.Context(), id, key, hash); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	_ = s.Store.WriteAudit(r.Context(), actor, "reset_api_key", "app", id, nil)
	// 完整 Key 仅本次返回；之后页面展示脱敏前缀，需再次重置才能取到新值。
	writeJSON(w, http.StatusOK, map[string]string{"key": key})
}
