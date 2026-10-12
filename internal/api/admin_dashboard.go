package api

import (
	"net/http"
	"strconv"
	"time"

	"adcenter/internal/store"
)

// dashboardOverviewResponse 总览页响应体：区间 KPI + 广告主 KPI 监控行。
type dashboardOverviewResponse struct {
	From        string                      `json:"from"`        // YYYY-MM-DD（含，当天 00:00:00 UTC 起）
	To          string                      `json:"to"`          // YYYY-MM-DD（含，截至当天 23:59:59 UTC）
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
	Days int                    `json:"days"`
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
