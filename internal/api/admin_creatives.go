package api

import (
	"net/http"
	"regexp"

	"adcenter/internal/store"
)

// storagePathRe 校验 R2 对象 key：creatives/{内容SHA-256}.{ext}
// 扁平内容寻址：key 即内容指纹，无广告主前缀；跨广告主相同素材共享同一条对象。
var storagePathRe = regexp.MustCompile(`^creatives/[0-9a-f]{64}\.[a-z0-9]+$`)

var mediaTypes = map[string]bool{"video": true, "image": true, "html": true}
var orientations = map[string]bool{"portrait": true, "landscape": true, "square": true, "any": true}

// creativeRequest 素材创建/更新请求体。
type creativeRequest struct {
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
	for _, c := range list {
		if c.MediaType == "html" {
			c.StorageURL = c.StoragePath
		} else if s.Storage != nil {
			c.StorageURL = s.Storage.PresignGET(c.StoragePath, s.Storage.DefaultExpiry())
		}
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
		"name":       req.Name,
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
