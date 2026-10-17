package api

import (
	"net/http"
	"sort"
	"strconv"
	"time"

	"adcenter/internal/store"
)

// dashboardOverviewResponse 总览页响应体：区间 KPI + 广告主 KPI 监控行。
type dashboardOverviewResponse struct {
	From        string                      `json:"from"` // YYYY-MM-DD（含，当天 00:00:00 UTC 起）
	To          string                      `json:"to"`   // YYYY-MM-DD（含，截至当天 23:59:59 UTC）
	Overview    *store.DashboardOverview    `json:"overview"`
	Advertisers []store.DashboardAdvertiser `json:"advertisers"`
}

// handleDashboardOverview 总览页 KPI（按日期区间，前端限制最多跨 7 天）。
//
//	GET /v1/admin/dashboard/overview?from=YYYY-MM-DD&to=YYYY-MM-DD
//
// 日期按 UTC 解释（全局口径，不按单个任务时区）：from 当天 00:00:00 起、to 当天 23:59:59 止；
// 省略则默认今日。请求/填充/曝光/点击/消耗来自 metrics_minute；安装/激活/注册/转化/付费
// 来自 ad_conversions（转化=first_purchase 首充，付费=purchase）。
// 广告主 KPI 监控为「今日」快照（预算/消耗无历史），不随区间变化。
func (s *Server) handleDashboardOverview(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireRole(w, r, false, false); !ok {
		return
	}
	q := r.URL.Query()
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	from := today
	to := today
	if y, m, d, ok := parseDate(q.Get("from")); ok {
		from = time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	}
	if y, m, d, ok := parseDate(q.Get("to")); ok {
		to = time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	}
	if to.Before(from) {
		from, to = to, from // 起止写反时自动纠正，避免查不到数据
	}
	// 右开区间：覆盖 to 当天 23:59:59，故结束取 to+1 天 00:00。
	toExclusive := to.AddDate(0, 0, 1)

	ov, err := s.Store.DashboardOverviewRange(r.Context(), from, toExclusive)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	advs, err := s.Store.DashboardAdvertisers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, dashboardOverviewResponse{
		From:        from.Format("2006-01-02"),
		To:          to.Format("2006-01-02"),
		Overview:    ov,
		Advertisers: advs,
	})
}

// dashboardDailyResponse 按日统计响应体（近 N 天每日全局聚合）。
type dashboardDailyResponse struct {
	Days int                       `json:"days"`
	Data []store.DashboardDailyRow `json:"data"`
}

// handleDashboardDaily 总览页「按日统计」：最近 N 天（默认 7）的全局每日聚合，
// 用于展示曝光/点击/CTR 的近期趋势（固定窗口，不随上方日期区间变化）。
//
//	GET /v1/admin/dashboard/daily?days=7
func (s *Server) handleDashboardDaily(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireRole(w, r, false, false); !ok {
		return
	}
	days := 7
	if v := r.URL.Query().Get("days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			days = n
		}
	}
	data, err := s.Store.DashboardDaily(r.Context(), days)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, dashboardDailyResponse{Days: days, Data: data})
}

// dashboardCampaignsResponse 按广告任务维度的总览响应体。
type dashboardCampaignsResponse struct {
	From      string                        `json:"from"`
	To        string                        `json:"to"`
	Days      []store.DashboardDay          `json:"days"`      // 按日分组（UTC）+ 素材明细
	Campaigns []store.DashboardCampaignStat `json:"campaigns"` // 每任务汇总 + 按日趋势
}

// handleDashboardCampaigns 总览页「按广告任务维度」统计：每个广告任务的
// 曝光/点击（ad_events）+ 安装/激活/注册/转化/付费（ad_conversions）+ 按日趋势（任务时区）。
// 不再做全局汇总，所有指标按 campaign 维度拆分。任务来源为 DB 全量未删除任务（含已停止），
// 再按窗口内实际活动过滤，因此「仅今日投放中、但 7 天内刚停的任务」也能统计出来。
//
//	GET /v1/admin/dashboard/campaigns?from=YYYY-MM-DD&to=YYYY-MM-DD
func (s *Server) handleDashboardCampaigns(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireRole(w, r, false, false); !ok {
		return
	}
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	from := today.AddDate(0, 0, -6) // 默认最近 7 天（与日期选择器默认一致）
	to := today
	if y, m, d, ok := parseDate(r.URL.Query().Get("from")); ok {
		from = time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	}
	if y, m, d, ok := parseDate(r.URL.Query().Get("to")); ok {
		to = time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	}
	if to.Before(from) {
		from, to = to, from
	}

	// 取全部未删除的广告任务（含已停止），再按窗口内实际活动过滤，
	// 这样「仅今日投放中、但 7 天内刚停的任务」也能统计出来。
	all, err := s.Store.ListCampaigns(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// 收集素材 id 用于取名称（含非活跃素材）
	crSet := map[string]struct{}{}
	for _, c := range all {
		for _, cr := range c.CreativeIDs {
			crSet[cr] = struct{}{}
		}
	}
	crNames := map[string]string{}
	if len(crSet) > 0 {
		ids := make([]string, 0, len(crSet))
		for id := range crSet {
			ids = append(ids, id)
		}
		if nm, e := s.Store.CreativeNameMap(r.Context(), ids); e == nil {
			crNames = nm
		}
	}
	camps := make([]store.CampaignDashInfo, 0, len(all))
	for _, c := range all {
		names := make(map[string]string, len(c.CreativeIDs))
		for _, cr := range c.CreativeIDs {
			names[cr] = crNames[cr]
		}
		camps = append(camps, store.CampaignDashInfo{
			ID: c.ID, Name: c.Name, Status: c.Status, Timezone: c.Timezone,
			CreativeIDs: c.CreativeIDs, CreativeNames: names,
		})
	}
	// 按名称稳定排序，便于前端展示
	sort.Slice(camps, func(i, j int) bool { return camps[i].Name < camps[j].Name })

	res, err := s.Store.DashboardCampaigns(r.Context(), camps, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// 过滤：仅保留「当前投放中」或「窗口内任一指标有活动」的任务。
	keep := make(map[string]bool, len(res.Campaigns))
	for _, st := range res.Campaigns {
		active := false
		for _, c := range camps {
			if c.ID == st.CampaignID && c.Status == "active" {
				active = true
				break
			}
		}
		any := st.Impressions > 0 || st.Clicks > 0 || st.Install > 0 ||
			st.Activate > 0 || st.Register > 0 || st.Conversion > 0 || st.Pay > 0
		hasDaily := false
		for _, d := range st.Daily {
			if d.Impressions > 0 || d.Clicks > 0 {
				hasDaily = true
				break
			}
		}
		if active || any || hasDaily {
			keep[st.CampaignID] = true
		}
	}
	filtered := make([]store.DashboardCampaignStat, 0, len(res.Campaigns))
	for _, st := range res.Campaigns {
		if keep[st.CampaignID] {
			filtered = append(filtered, st)
		}
	}
	days := make([]store.DashboardDay, 0, len(res.Days))
	for _, dy := range res.Days {
		cc := make([]store.DashboardCampaignDay, 0, len(dy.Campaigns))
		for _, c := range dy.Campaigns {
			if keep[c.CampaignID] {
				cc = append(cc, c)
			}
		}
		if len(cc) > 0 {
			days = append(days, store.DashboardDay{Date: dy.Date, Campaigns: cc})
		}
	}

	writeJSON(w, http.StatusOK, dashboardCampaignsResponse{
		From:      from.Format("2006-01-02"),
		To:        to.Format("2006-01-02"),
		Days:      days,
		Campaigns: filtered,
	})
}
