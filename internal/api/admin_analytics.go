package api

import (
	"net/http"
	"sort"
	"strings"
	"time"
)

// creativeStatItem 单素材曝光/点击/CTR（含素材名，便于前端直接展示）。
type creativeStatItem struct {
	CreativeID  string  `json:"creative_id"`
	Name        string  `json:"name"`
	Impressions int64   `json:"impressions"`
	Clicks      int64   `json:"clicks"`
	CTR         float64 `json:"ctr"` // 点击率 0~1（分母=曝光）
}

// campaignCreativeStats 一个广告任务（或全部素材）的素材级数据分析。
type campaignCreativeStats struct {
	CampaignID string             `json:"campaign_id"` // 全局模式为空字符串
	From       string             `json:"from"`
	To         string             `json:"to"`
	Totals     creativeStatItem   `json:"totals"`
	Creatives  []creativeStatItem `json:"creatives"`
}

// handleCampaignCreativeStats 按素材聚合曝光/点击/点击率（数据源 ad_events）。
//
//	GET /v1/admin/analytics/creatives?campaign_id=&from=&to=
//
// campaign_id 省略时为「全部素材」模式；from/to 省略默认当天 00:00(UTC) ~ 现在。
// 口径：曝光=impression 事件，点击=click 事件（fill 下发不计入）。
func (s *Server) handleCampaignCreativeStats(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireRole(w, r, false, false); !ok {
		return
	}
	q := r.URL.Query()
	campaignID := strings.TrimSpace(q.Get("campaign_id"))

	now := time.Now().UTC()
	from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	to := now
	if v := q.Get("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			from = t
		} else if t, err := time.Parse("2006-01-02", v); err == nil {
			from = t
		}
	}
	if v := q.Get("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			to = t
		} else if t, err := time.Parse("2006-01-02", v); err == nil {
			// 含当天：截止当日 23:59:59
			to = t.Add(24*time.Hour - time.Second)
		}
	}

	snap := s.Cache.Snapshot()
	if snap == nil {
		writeError(w, http.StatusServiceUnavailable, "config not loaded")
		return
	}

	// 解析要统计的素材集合 + 名称映射
	var creativeIDs []string
	nameMap := make(map[string]string)
	if campaignID != "" {
		camp := snap.Campaigns[campaignID]
		if camp == nil {
			writeError(w, http.StatusNotFound, "campaign not found")
			return
		}
		for _, id := range camp.CreativeIDs {
			creativeIDs = append(creativeIDs, id)
			if cr, ok := snap.CreativesByID[id]; ok {
				nameMap[id] = cr.Name
			} else {
				nameMap[id] = id
			}
		}
	} else {
		for id, cr := range snap.CreativesByID {
			creativeIDs = append(creativeIDs, id)
			nameMap[id] = cr.Name
		}
	}

	stats, err := s.Store.CreativeStats(r.Context(), creativeIDs, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	items := make([]creativeStatItem, 0, len(creativeIDs))
	var tot creativeStatItem
	for _, id := range creativeIDs {
		imp, clk := int64(0), int64(0)
		if st := stats[id]; st != nil {
			imp, clk = st.Impressions, st.Clicks
		}
		// 全局模式只展示有数据的素材；任务模式展示全部关联素材（含 0）
		if campaignID != "" || imp+clk > 0 {
			it := creativeStatItem{
				CreativeID:  id,
				Name:        nameMap[id],
				Impressions: imp,
				Clicks:      clk,
			}
			if imp > 0 {
				it.CTR = float64(clk) / float64(imp)
			}
			items = append(items, it)
			tot.Impressions += imp
			tot.Clicks += clk
		}
	}
	// 排序：点击率降序，再按曝光降序（方便找出"高点击率素材"）
	sort.Slice(items, func(i, j int) bool {
		if items[i].CTR != items[j].CTR {
			return items[i].CTR > items[j].CTR
		}
		return items[i].Impressions > items[j].Impressions
	})
	if tot.Impressions > 0 {
		tot.CTR = float64(tot.Clicks) / float64(tot.Impressions)
	}
	tot.Name = "合计"

	resp := campaignCreativeStats{
		CampaignID: campaignID,
		From:       from.Format(time.RFC3339),
		To:         to.Format(time.RFC3339),
		Totals:     tot,
		Creatives:  items,
	}
	writeJSON(w, http.StatusOK, resp)
}
