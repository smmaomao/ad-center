package api

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"adcenter/internal/config"
	_ "time/tzdata"
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
	Timezone   string             `json:"timezone"`    // 统计时区（IANA 名称）
	From       string             `json:"from"`
	To         string             `json:"to"`
	Totals     creativeStatItem   `json:"totals"`
	Creatives  []creativeStatItem `json:"creatives"`
}

// handleCampaignCreativeStats 按素材聚合曝光/点击/点击率（数据源 ad_events）。
//
//	GET /v1/admin/analytics/creatives?campaign_id=&from=&to=
//
// campaign_id 省略时为「全部素材」模式（按 UTC）；否则按该任务的 timezone 字段
// 解释日期边界（纯日期默认该时区当天 00:00 ~ 23:59:59）。from/to 省略默认该时区当天 ~ 现在。
// 口径：曝光=impression 事件，点击=click 事件（fill 下发不计入）。
func (s *Server) handleCampaignCreativeStats(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireRole(w, r, false, false); !ok {
		return
	}
	q := r.URL.Query()
	campaignID := strings.TrimSpace(q.Get("campaign_id"))

	snap := s.Cache.Snapshot()
	if snap == nil {
		writeError(w, http.StatusServiceUnavailable, "config not loaded")
		return
	}

	// 统计时区：按广告任务配置，缺省 UTC；全局模式（无 campaign_id）按 UTC。
	tzName := "UTC"
	var camp *config.Campaign
	if campaignID != "" {
		camp = snap.Campaigns[campaignID]
		if camp == nil {
			writeError(w, http.StatusNotFound, "campaign not found")
			return
		}
		if camp.Timezone != "" {
			tzName = camp.Timezone
		}
	}
	loc, err := time.LoadLocation(tzName)
	if err != nil {
		loc = time.UTC // 兜底：理论上不会发生（创建时已校验时区名）
	}

	// 日期边界按任务时区解释：纯日期（YYYY-MM-DD）当作该时区当天 00:00/23:59:59；
	// 带时区的时间戳（RFC3339）直接使用其绝对时刻。
	now := time.Now().In(loc)
	from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	to := now
	if v := q.Get("from"); v != "" {
		if t, perr := time.Parse(time.RFC3339, v); perr == nil {
			from = t
		} else if y, m, d, ok := parseDate(v); ok {
			from = time.Date(y, m, d, 0, 0, 0, 0, loc)
		}
	}
	if v := q.Get("to"); v != "" {
		if t, perr := time.Parse(time.RFC3339, v); perr == nil {
			to = t
		} else if y, m, d, ok := parseDate(v); ok {
			to = time.Date(y, m, d, 23, 59, 59, 0, loc)
		}
	}

	// 解析要统计的素材集合 + 名称映射
	var creativeIDs []string
	nameMap := make(map[string]string)
	if camp != nil {
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
		Timezone:   tzName,
		From:       from.Format(time.RFC3339),
		To:         to.Format(time.RFC3339),
		Totals:     tot,
		Creatives:  items,
	}
	writeJSON(w, http.StatusOK, resp)
}

// parseDate 解析 YYYY-MM-DD 为年/月/日（无时区，调用方需指定解释时区）。
func parseDate(s string) (year int, month time.Month, day int, ok bool) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return 0, 0, 0, false
	}
	return t.Year(), t.Month(), t.Day(), true
}
