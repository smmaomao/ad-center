package api

import (
	"net/http"
	"regexp"

	"adcenter/internal/store"
)

// ============================================================
// 广告位 CRUD + 素材 CRUD（管理 API，BFF 转发）
// ============================================================

var slotKeyRe = regexp.MustCompile(`^[a-z0-9_]{2,64}$`)
var guaranteedSumLimit = 1.0

// slotRequest 广告位创建/更新请求体。
type slotRequest struct {
	AppID               string                `json:"app_code"`
	SlotKey             string                `json:"slot_key"`
	Name                string                `json:"name"`
	Type                string                `json:"type"`
	Status              string                `json:"status"`
	FreqDailyLimit      int                   `json:"freq_daily_limit"`
	FreqIntervalMinutes int                   `json:"freq_interval_minutes"`
	FreqFatigueWindow   int                   `json:"freq_fatigue_window"`
	AIAgentEnabled      bool                  `json:"ai_agent_enabled"`
	AIAgentGoal         string                `json:"ai_agent_goal"`
	UpdatedBy           string                `json:"updated_by"`
	Priorities          []store.PriorityInput `json:"priorities"`
}

var slotTypes = map[string]bool{
	"rewarded_video": true, "splash": true, "interstitial": true, "feed": true,
}

func validatePriorities(ps []store.PriorityInput) (float64, bool) {
	var guaranteedSum float64
	for _, p := range ps {
		switch p.SourceType {
		case "advertiser":
			if p.AdvertiserID == "" {
				return 0, false
			}
		case "max", "fallback":
			// 无广告主
		default:
			return 0, false
		}
		if p.GuaranteedShare < 0 || p.GuaranteedShare > 1 {
			return 0, false
		}
		guaranteedSum += p.GuaranteedShare
	}
	return guaranteedSum, true
}

// handleCreateSlot 新建广告位（operator 及以上）。
func (s *Server) handleCreateSlot(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireRole(w, r, true, false)
	if !ok {
		return
	}
	var req slotRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.AppID == "" || req.Name == "" || !slotTypes[req.Type] {
		writeError(w, http.StatusBadRequest, "app_code, name, valid type required")
		return
	}
	if !slotKeyRe.MatchString(req.SlotKey) {
		writeError(w, http.StatusBadRequest, "slot_key must match ^[a-z0-9_]{2,64}$")
		return
	}
	if sum, ok2 := validatePriorities(req.Priorities); !ok2 || sum > guaranteedSumLimit {
		writeError(w, http.StatusBadRequest, "invalid priorities (check source_type/advertiser/guaranteed_share, sum ≤ 1)")
		return
	}

	fields := map[string]any{
		"app_code": req.AppID, "slot_key": req.SlotKey, "name": req.Name,
		"type": req.Type, "updated_by": actor, "created_by": actor,
		"ai_agent_enabled": req.AIAgentEnabled,
	}
	if req.Status != "" {
		fields["status"] = req.Status
	}
	if req.FreqDailyLimit > 0 {
		fields["freq_daily_limit"] = req.FreqDailyLimit
	}
	if req.FreqIntervalMinutes > 0 {
		fields["freq_interval_minutes"] = req.FreqIntervalMinutes
	}
	if req.FreqFatigueWindow > 0 {
		fields["freq_fatigue_window"] = req.FreqFatigueWindow
	}
	if req.AIAgentGoal != "" {
		fields["ai_agent_goal"] = req.AIAgentGoal
	}

	id, err := s.Store.CreateSlot(r.Context(), fields, req.Priorities)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	_ = s.Store.WriteAudit(r.Context(), actor, "create", "slot", id, nil)
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

// handleGetSlot 广告位详情（含优先级）。
func (s *Server) handleGetSlot(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireRole(w, r, false, false); !ok {
		return
	}
	d, err := s.Store.GetSlotDetail(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// handleUpdateSlot 更新广告位；priorities 非 nil 时全量替换（拖拽排序语义）。
// slot_key/app_code 不可改（客户端稳定标识）。
func (s *Server) handleUpdateSlot(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireRole(w, r, true, false)
	if !ok {
		return
	}
	id := r.PathValue("id")
	var req slotRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	fields := map[string]any{"updated_by": actor}
	if req.Name != "" {
		fields["name"] = req.Name
	}
	if req.Status != "" {
		fields["status"] = req.Status
	}
	if req.Type != "" {
		if !slotTypes[req.Type] {
			writeError(w, http.StatusBadRequest, "invalid type")
			return
		}
		fields["type"] = req.Type
	}
	if req.FreqDailyLimit > 0 {
		fields["freq_daily_limit"] = req.FreqDailyLimit
	}
	if req.FreqIntervalMinutes > 0 {
		fields["freq_interval_minutes"] = req.FreqIntervalMinutes
	}
	if req.FreqFatigueWindow > 0 {
		fields["freq_fatigue_window"] = req.FreqFatigueWindow
	}
	if req.AIAgentGoal != "" {
		fields["ai_agent_goal"] = req.AIAgentGoal
	}
	fields["ai_agent_enabled"] = req.AIAgentEnabled

	if sum, ok2 := validatePriorities(req.Priorities); !ok2 || sum > guaranteedSumLimit {
		writeError(w, http.StatusBadRequest, "invalid priorities (check source_type/advertiser/guaranteed_share, sum ≤ 1)")
		return
	}

	if err := s.Store.UpdateSlot(r.Context(), id, fields, req.Priorities, req.Priorities != nil); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	_ = s.Store.WriteAudit(r.Context(), actor, "update", "slot", id, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleDeleteSlot 软删广告位（super_admin）。
func (s *Server) handleDeleteSlot(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireRole(w, r, true, true)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if err := s.Store.SoftDeleteSlot(r.Context(), id); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	_ = s.Store.WriteAudit(r.Context(), actor, "delete", "slot", id, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ============================================================
// 素材 CRUD（上传走 Next.js presigned PUT，这里注册元数据）
// ============================================================

var mediaTypes = map[string]bool{"video": true, "image": true, "html": true}
var orientations = map[string]bool{"portrait": true, "landscape": true, "square": true, "any": true}

// storagePathRe 校验 R2 对象 key：creatives/{内容SHA-256}.{ext}
// 扁平内容寻址：key 即内容指纹，无广告主前缀；跨广告主相同素材共享同一条对象。
var storagePathRe = regexp.MustCompile(`^creatives/[0-9a-f]{64}\.[a-z0-9]+$`)

// creativeRequest 素材创建/更新请求体。
type creativeRequest struct {
	AdvertiserID  string   `json:"advertiser_id"`
	Name          string   `json:"name"`
	MediaType     string   `json:"media_type"`
	StoragePath   string   `json:"storage_path"`
	FileSizeBytes int64    `json:"file_size_bytes"`
	Orientation   string   `json:"orientation"`
	Width         int      `json:"width"`
	Height        int      `json:"height"`
	DurationMS    int      `json:"duration_ms"`
	Status        string   `json:"status"`
	Styles        []string `json:"styles"`
	TargetApps    []string `json:"target_apps"`
	ABGroup       string   `json:"ab_group"`
}

// handleListCreatives 素材列表（可按广告主过滤）。
func (s *Server) handleListCreatives(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireRole(w, r, false, false); !ok {
		return
	}
	list, err := s.Store.ListCreatives(r.Context(), r.URL.Query().Get("advertiser_id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if list == nil {
		list = []*store.AdminCreative{}
	}
	writeJSON(w, http.StatusOK, list)
}

// handleCreateCreative 注册素材元数据（R2 直传完成后调用）。
func (s *Server) handleCreateCreative(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireRole(w, r, true, false)
	if !ok {
		return
	}
	var req creativeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Name == "" || !mediaTypes[req.MediaType] {
		writeError(w, http.StatusBadRequest, "name, valid media_type required")
		return
	}
	// html 存完整 URL；video/image 必须是 R2 扁平内容寻址对象 key（防任意路径注入）
	if req.MediaType == "html" {
		if len(req.StoragePath) < 8 || (req.StoragePath[:7] != "http://" && req.StoragePath[:8] != "https://") {
			writeError(w, http.StatusBadRequest, "html storage_path must be a URL")
			return
		}
	} else if !storagePathRe.MatchString(req.StoragePath) {
		writeError(w, http.StatusBadRequest,
			"storage_path must match creatives/{sha256}.ext")
		return
	}
	if req.Orientation != "" && !orientations[req.Orientation] {
		writeError(w, http.StatusBadRequest, "invalid orientation")
		return
	}

	fields := map[string]any{
		"advertiser_id": req.AdvertiserID, "name": req.Name,
		"media_type": req.MediaType, "storage_path": req.StoragePath,
		"file_size_bytes": req.FileSizeBytes, "updated_by": actor,
	}
	if req.Orientation != "" {
		fields["orientation"] = req.Orientation
	}
	if req.Width > 0 {
		fields["width"] = req.Width
	}
	if req.Height > 0 {
		fields["height"] = req.Height
	}
	if req.DurationMS > 0 {
		fields["duration_ms"] = req.DurationMS
	}
	if req.Status != "" {
		fields["status"] = req.Status
	}
	if req.ABGroup != "" {
		fields["ab_group"] = req.ABGroup
	}
	if len(req.Styles) > 0 {
		fields["styles"] = req.Styles
	}
	if len(req.TargetApps) > 0 {
		fields["target_apps"] = req.TargetApps
	}

	id, err := s.Store.CreateCreative(r.Context(), fields)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	_ = s.Store.WriteAudit(r.Context(), actor, "create", "creative", id, nil)
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

// handleUpdateCreative 更新素材（权重/A-B 分组/状态等投放属性）。
func (s *Server) handleUpdateCreative(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireRole(w, r, true, false)
	if !ok {
		return
	}
	id := r.PathValue("id")
	var req creativeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	fields := map[string]any{"updated_by": actor}
	if req.MediaType != "" {
		if !mediaTypes[req.MediaType] {
			writeError(w, http.StatusBadRequest, "invalid media_type")
			return
		}
		fields["media_type"] = req.MediaType
	}
	if req.Name != "" {
		fields["name"] = req.Name
	}
	if req.StoragePath != "" {
		fields["storage_path"] = req.StoragePath
	}
	if req.Status != "" {
		fields["status"] = req.Status
	}
	if req.ABGroup != "" {
		fields["ab_group"] = req.ABGroup
	}
	if req.Orientation != "" {
		if !orientations[req.Orientation] {
			writeError(w, http.StatusBadRequest, "invalid orientation")
			return
		}
		fields["orientation"] = req.Orientation
	}
	if req.Width > 0 {
		fields["width"] = req.Width
	}
	if req.Height > 0 {
		fields["height"] = req.Height
	}
	if req.DurationMS > 0 {
		fields["duration_ms"] = req.DurationMS
	}
	if len(req.Styles) > 0 {
		fields["styles"] = req.Styles
	}
	if len(req.TargetApps) > 0 {
		fields["target_apps"] = req.TargetApps
	}

	if err := s.Store.UpdateCreative(r.Context(), id, fields); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	_ = s.Store.WriteAudit(r.Context(), actor, "update", "creative", id, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleDeleteCreative 软删素材。
func (s *Server) handleDeleteCreative(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireRole(w, r, true, false)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if err := s.Store.SoftDeleteCreative(r.Context(), id); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	_ = s.Store.WriteAudit(r.Context(), actor, "delete", "creative", id, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleCreativePlayURL 后台预览：把素材 storage_path 转成可播放地址。
// video/image 走 R2 预签名 GET（与决策下发同套签名逻辑）；html 存的是完整 URL，直链不签名。
func (s *Server) handleCreativePlayURL(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireRole(w, r, false, false); !ok {
		return
	}
	id := r.PathValue("id")
	c, err := s.Store.GetCreative(r.Context(), id)
	if err != nil || c == nil {
		writeError(w, http.StatusNotFound, "creative not found")
		return
	}
	var url string
	switch c.MediaType {
	case "html":
		url = c.StoragePath // 落地页外链，直接播
	case "video", "image":
		if s.Storage == nil {
			writeError(w, http.StatusNotImplemented, "storage (R2) not configured")
			return
		}
		url = s.Storage.PresignGET(c.StoragePath, s.Storage.DefaultExpiry())
	default:
		writeError(w, http.StatusBadRequest, "unsupported media_type: "+c.MediaType)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"url":        url,
		"media_type": c.MediaType,
	})
}
