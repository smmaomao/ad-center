package api

import (
	"encoding/json"
	"net/http"

	"adcenter/internal/config"
)

// handleGetSetting 读取某运行时设置项的原始 JSON（只读角色可访问）。
//
//	GET /v1/admin/settings/{key}
func (s *Server) handleGetSetting(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireRole(w, r, false, false); !ok {
		return
	}
	key := r.PathValue("key")
	raw, err := s.Store.GetSetting(r.Context(), key)
	if err != nil {
		writeError(w, http.StatusNotFound, "setting not found: "+key)
		return
	}
	writeJSON(w, http.StatusOK, map[string]json.RawMessage{"value": raw})
}

// handleUpdateSetting 更新某运行时设置项（写角色可访问）。
//
//	PATCH /v1/admin/settings/{key}
//	body: {"value": <json>}
//
// 当前仅开放 decision_cache（决策缓存配置）；写入后 settings 表触发器触发
// notify_config_changed，配置快照秒级热更新，无需重启。
func (s *Server) handleUpdateSetting(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireRole(w, r, true, false); !ok {
		return
	}
	key := r.PathValue("key")
	if key != "decision_cache" && key != "pricing_benchmark" {
		writeError(w, http.StatusBadRequest, "unknown setting: "+key)
		return
	}
	var body struct {
		Value json.RawMessage `json:"value"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}

	// 平台计费标准线：素材出价基准分的分母（系统设置 → 平台计费标准线）。
	// 改完配置快照热更新即生效，打分基准随之变化，无需重启。
	if key == "pricing_benchmark" {
		var b config.PricingBenchmark
		if err := json.Unmarshal(body.Value, &b); err != nil {
			writeError(w, http.StatusBadRequest, "invalid pricing_benchmark: "+err.Error())
			return
		}
		// 六项标准线都是分母，必须为正——否则出价基准分会被放大成天文数字
		if b.CPM <= 0 || b.CPC <= 0 || b.CPAInstall <= 0 ||
			b.CPAActivate <= 0 || b.CPARegister <= 0 || b.CPAFirstPurchase <= 0 {
			writeError(w, http.StatusBadRequest,
				"六项标准线都必须为正数（它们作为出价基准分的分母，不能为 0 或负）")
			return
		}
		if err := s.Store.UpsertSetting(r.Context(), key, body.Value); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	var cfg config.DecisionCacheConfig
	if err := json.Unmarshal(body.Value, &cfg); err != nil {
		writeError(w, http.StatusBadRequest, "invalid decision_cache config: "+err.Error())
		return
	}
	if cfg.TTLSeconds <= 0 || cfg.TTLSeconds > 3600 {
		writeError(w, http.StatusBadRequest, "ttl_seconds must be in (0, 3600]")
		return
	}
	if err := s.Store.UpsertSetting(r.Context(), key, body.Value); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
