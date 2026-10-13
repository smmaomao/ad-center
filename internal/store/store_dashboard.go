package store

import (
	"context"
	"math"
	"strings"
	"time"
)

// ============================================================
// 看板（阶段 3.1）聚合查询：总览 KPI / 广告主 KPI 监控 / 广告位实时状态
// 数据源：campaigns（预算·消耗·达成）+ metrics_minute（今日曝光点击等）
// 口径与 CampaignRollup 一致（target/actual 加权，actual≤0 取 1，clamp [0.25,4]）
// ============================================================

// DashboardOverview 看板顶部 KPI 卡。
type DashboardOverview struct {
	DailyBudget       float64 `json:"daily_budget"`
	SpentToday        float64 `json:"spent_today"`
	Achievement       float64 `json:"achievement"`     // 总体达成率（加权）
	Requests          int64   `json:"requests"`        // 今日请求
	Fills             int64   `json:"fills"`           // 今日填充
	Impressions       int64   `json:"impressions"`     // 今日曝光
	Clicks            int64   `json:"clicks"`          // 今日点击
	Conversions       int64   `json:"conversions"`     // 今日转化
	Revenue           float64 `json:"revenue"`         // 今日消耗金额
	FillRate          float64 `json:"fill_rate"`       // fills/requests
	ECPM              float64 `json:"ecpm"`            // revenue/impressions*1000
	ActiveAdvertisers int     `json:"active_advertisers"`
	ActiveCampaigns   int     `json:"active_campaigns"`
	// 转化事件拆分（数据源 ad_conversions，S2S 回传明细，按 event_type 聚合）：
	// 安装=install、激活=activate、注册=register、转化=first_purchase(首充)、付费=purchase。
	Install    int64 `json:"install"`
	Activate   int64 `json:"activate"`
	Register   int64 `json:"register"`
	Conversion int64 `json:"conversion"` // first_purchase
	Pay        int64 `json:"pay"`        // purchase
}

// DashboardDailyRow 按日统计的一行（近 N 天，全局口径，UTC）。
type DashboardDailyRow struct {
	Date        string  `json:"date"`        // YYYY-MM-DD
	Impressions int64   `json:"impressions"` // 当日曝光
	Clicks      int64   `json:"clicks"`      // 当日点击
	Conversions int64   `json:"conversions"` // 当日转化（首充）
	Revenue     float64 `json:"revenue"`     // 当日消耗金额
}

// DashboardDaily 拉取最近 days 天（含今天）的全局每日聚合，按日期升序，
// 缺失数据的日期也补齐为 0 行（generate_series 保证连续 7 天）。
// 数据源 metrics_minute（分区表，保留 13 个月），与总览 KPI 口径一致。
func (s *Store) DashboardDaily(ctx context.Context, days int) ([]DashboardDailyRow, error) {
	if days < 1 {
		days = 1
	}
	if days > 92 {
		days = 92
	}
	rows, err := s.pool.Query(ctx, `
		SELECT to_char(d.day, 'YYYY-MM-DD'),
		       COALESCE(SUM(m.impressions), 0)::bigint,
		       COALESCE(SUM(m.clicks), 0)::bigint,
		       COALESCE(SUM(m.conversions), 0)::bigint,
		       COALESCE(SUM(m.revenue), 0)::float8
		FROM generate_series(
		       date_trunc('day', now()) - make_interval(days => $1 - 1),
		       date_trunc('day', now()),
		       interval '1 day'
		     ) AS d(day)
		LEFT JOIN metrics_minute m
		       ON m.minute_ts >= d.day AND m.minute_ts < d.day + interval '1 day'
		GROUP BY d.day
		ORDER BY d.day`, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]DashboardDailyRow, 0, days)
	for rows.Next() {
		var r DashboardDailyRow
		if err := rows.Scan(&r.Date, &r.Impressions, &r.Clicks, &r.Conversions, &r.Revenue); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// CampaignDashInfo 调用方传入的广告任务概要（来自 DB，避免 store 依赖 config）。
type CampaignDashInfo struct {
	ID            string
	Name          string
	Status        string // active / paused（用于区分投放中/已停止）
	Timezone      string
	CreativeIDs   []string
	CreativeNames map[string]string // creative_id -> 素材名（明细展示用）
}

// DashboardCampaignDaily 单日曝光/点击（按该任务时区切分）。
type DashboardCampaignDaily struct {
	Date        string `json:"date"`        // YYYY-MM-DD（任务时区）
	Impressions int64  `json:"impressions"` // 当日曝光
	Clicks      int64  `json:"clicks"`      // 当日点击
}

// DashboardCreativeDaily 单日单个素材的曝光/点击/CTR（归属到某任务）。
type DashboardCreativeDaily struct {
	CreativeID  string  `json:"creative_id"`
	Name        string  `json:"name"`
	Impressions int64   `json:"impressions"`
	Clicks      int64   `json:"clicks"`
	Ctr         float64 `json:"ctr"` // 0~1
}

// DashboardCampaignDay 单个广告任务在单日（UTC）的指标 + 关联素材明细。
type DashboardCampaignDay struct {
	CampaignID  string                   `json:"campaign_id"`
	Name        string                   `json:"name"`
	Timezone    string                   `json:"timezone"`
	Status      string                   `json:"status"`
	Impressions int64                    `json:"impressions"`
	Clicks      int64                    `json:"clicks"`
	Install     int64                    `json:"install"`
	Activate    int64                    `json:"activate"`
	Register    int64                    `json:"register"`
	Conversion  int64                    `json:"conversion"`
	Pay         int64                    `json:"pay"`
	Creatives   []DashboardCreativeDaily `json:"creatives"`
}

// DashboardDay 一天（UTC 日期）下各广告任务的指标（用于「按日分组」表格）。
type DashboardDay struct {
	Date      string               `json:"date"` // YYYY-MM-DD
	Campaigns []DashboardCampaignDay `json:"campaigns"`
}

// DashboardCampaignStat 单个广告任务在 [from,to] 内的全部指标（按任务维度统计）。
type DashboardCampaignStat struct {
	CampaignID  string                   `json:"campaign_id"`
	Name        string                   `json:"name"`
	Timezone    string                   `json:"timezone"`
	Status      string                   `json:"status"`
	Impressions int64                    `json:"impressions"` // 曝光
	Clicks      int64                    `json:"clicks"`      // 点击
	Install     int64                    `json:"install"`     // 安装
	Activate    int64                    `json:"activate"`    // 激活
	Register    int64                    `json:"register"`    // 注册
	Conversion  int64                    `json:"conversion"`  // 转化（首充 first_purchase）
	Pay         int64                    `json:"pay"`         // 付费（purchase）
	Daily       []DashboardCampaignDaily `json:"daily"`       // 按日趋势（曝光/点击，任务时区）
}

// DashboardCampaignsResult 总览「按广告任务维度」结果：
// - Campaigns：每个任务在 [from,to] 的汇总指标 + 按日趋势（供趋势图）。
// - Days：按日分组（UTC），每天下列出当日有活动的任务及其关联素材明细（供分析表）。
// 口径：曝光/点击来自 ad_events（按 creative_id 归属任务）；安装/激活/注册/转化/付费来自
// ad_conversions（按 campaign_id）。当前量小实时查；量大后改离线预聚合（待办）。
func (s *Store) DashboardCampaigns(
	ctx context.Context, camps []CampaignDashInfo, from, to time.Time,
) (*DashboardCampaignsResult, error) {
	toExclusive := to.AddDate(0, 0, 1)
	stats := make([]DashboardCampaignStat, 0, len(camps))
	for _, c := range camps {
		st := DashboardCampaignStat{
			CampaignID: c.ID, Name: c.Name, Timezone: c.Timezone, Status: c.Status,
		}
		if len(c.CreativeIDs) > 0 {
			cs, err := s.CreativeStats(ctx, c.CreativeIDs, from, toExclusive)
			if err != nil {
				return nil, err
			}
			for _, v := range cs {
				st.Impressions += v.Impressions
				st.Clicks += v.Clicks
			}
		}
		conv, err := s.campaignConversions(ctx, c.ID, from, toExclusive)
		if err != nil {
			return nil, err
		}
		st.Install = conv["install"]
		st.Activate = conv["activate"]
		st.Register = conv["register"]
		st.Conversion = conv["first_purchase"]
		st.Pay = conv["purchase"]

		daily, err := s.campaignDaily(ctx, c.CreativeIDs, c.Timezone, from, to)
		if err != nil {
			return nil, err
		}
		st.Daily = daily
		stats = append(stats, st)
	}

	days, err := s.dashboardDays(ctx, camps, from, toExclusive)
	if err != nil {
		return nil, err
	}
	return &DashboardCampaignsResult{Campaigns: stats, Days: days}, nil
}

// DashboardCampaignsResult 见上。
type DashboardCampaignsResult struct {
	Campaigns []DashboardCampaignStat `json:"campaigns"`
	Days      []DashboardDay          `json:"days"`
}

// dashboardDays 按 UTC 日分组：对 [from,toEx) 内每一天，列出当日有活动的广告任务及其素材明细。
// 单条 SQL 批量拉取窗口内 ad_events / ad_conversions，按 (任务,日,素材) 在 Go 侧归集，
// 避免 N天×M任务 的笛卡尔查询。仅当日全指标为 0 的任务不出现在该日。
func (s *Store) dashboardDays(
	ctx context.Context, camps []CampaignDashInfo, from, toExclusive time.Time,
) ([]DashboardDay, error) {
	type evKey struct {
		camp int
		day  string
		cr   string
	}
	crToCamp := make(map[string]int, len(camps))
	allCr := make([]string, 0)
	for i, c := range camps {
		for _, cr := range c.CreativeIDs {
			if _, seen := crToCamp[cr]; !seen {
				crToCamp[cr] = i
				allCr = append(allCr, cr)
			}
		}
	}

	evImp := make(map[evKey]int64)
	evClk := make(map[evKey]int64)
	if len(allCr) > 0 {
		lit := "{" + strings.Join(allCr, ",") + "}"
		rows, err := s.pool.Query(ctx, `
			SELECT creative_id::text, to_char(ts AT TIME ZONE 'UTC', 'YYYY-MM-DD'), event_type, COUNT(*)::bigint
			FROM ad_events
			WHERE ts >= $1 AND ts < $2 AND event_type IN ('impression', 'click')
			      AND creative_id = ANY($3::bigint[])
			GROUP BY creative_id, to_char(ts AT TIME ZONE 'UTC', 'YYYY-MM-DD'), event_type`, from, toExclusive, lit)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var cr, day, et string
			var n int64
			if err := rows.Scan(&cr, &day, &et, &n); err != nil {
				rows.Close()
				return nil, err
			}
			ci, ok := crToCamp[cr]
			if !ok {
				continue
			}
			k := evKey{ci, day, cr}
			if et == "impression" {
				evImp[k] += n
			} else {
				evClk[k] += n
			}
		}
		rows.Close()
	}

	// 转化事件按 (任务,日) 归集
	conv := make(map[evKey]map[string]int64)
	campIDs := make([]string, 0, len(camps))
	for _, c := range camps {
		campIDs = append(campIDs, c.ID)
	}
	if len(campIDs) > 0 {
		lit := "{" + strings.Join(campIDs, ",") + "}"
		rows, err := s.pool.Query(ctx, `
			SELECT campaign_id::text, to_char(ts AT TIME ZONE 'UTC', 'YYYY-MM-DD'), event_type, COUNT(*)::bigint
			FROM ad_conversions
			WHERE ts >= $1 AND ts < $2 AND campaign_id = ANY($3::text[])
			GROUP BY campaign_id, to_char(ts AT TIME ZONE 'UTC', 'YYYY-MM-DD'), event_type`, from, toExclusive, lit)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var cid, day, et string
			var n int64
			if err := rows.Scan(&cid, &day, &et, &n); err != nil {
				rows.Close()
				return nil, err
			}
			ci := -1
			for i, c := range camps {
				if c.ID == cid {
					ci = i
					break
				}
			}
			if ci < 0 {
				continue
			}
			k := evKey{ci, day, ""}
			if conv[k] == nil {
				conv[k] = make(map[string]int64)
			}
			conv[k][et] += n
		}
		rows.Close()
	}

	out := make([]DashboardDay, 0)
	for d := from; d.Before(toExclusive); d = d.AddDate(0, 0, 1) {
		dayStr := d.Format("2006-01-02")
		dd := DashboardDay{Date: dayStr}
		for ci, c := range camps {
			imp, clk := int64(0), int64(0)
			for _, cr := range c.CreativeIDs {
				imp += evImp[evKey{ci, dayStr, cr}]
				clk += evClk[evKey{ci, dayStr, cr}]
			}
			cv := conv[evKey{ci, dayStr, ""}]
			inst := mapVal(cv, "install")
			act := mapVal(cv, "activate")
			reg := mapVal(cv, "register")
			conv1 := mapVal(cv, "first_purchase")
			pay := mapVal(cv, "purchase")
			if imp == 0 && clk == 0 && inst == 0 && act == 0 && reg == 0 && conv1 == 0 && pay == 0 {
				continue
			}
			creatives := make([]DashboardCreativeDaily, 0)
			for _, cr := range c.CreativeIDs {
				ci2 := evImp[evKey{ci, dayStr, cr}]
				cc2 := evClk[evKey{ci, dayStr, cr}]
				if ci2 == 0 && cc2 == 0 {
					continue
				}
				ctr := 0.0
				if ci2 > 0 {
					ctr = float64(cc2) / float64(ci2)
				}
				creatives = append(creatives, DashboardCreativeDaily{
					CreativeID:  cr,
					Name:        c.CreativeNames[cr],
					Impressions: ci2,
					Clicks:      cc2,
					Ctr:         ctr,
				})
			}
			dd.Campaigns = append(dd.Campaigns, DashboardCampaignDay{
				CampaignID:  c.ID,
				Name:        c.Name,
				Timezone:    c.Timezone,
				Status:      c.Status,
				Impressions: imp,
				Clicks:      clk,
				Install:     inst,
				Activate:    act,
				Register:    reg,
				Conversion:  conv1,
				Pay:         pay,
				Creatives:   creatives,
			})
		}
		if len(dd.Campaigns) > 0 {
			out = append(out, dd)
		}
	}
	return out, nil
}

func mapVal(m map[string]int64, k string) int64 {
	if m == nil {
		return 0
	}
	return m[k]
}

// CreativeNameMap 批量取素材 id -> 名称（含已停止/非活跃素材，供明细展示）。
func (s *Store) CreativeNameMap(ctx context.Context, ids []string) (map[string]string, error) {
	lit := "{" + strings.Join(ids, ",") + "}"
	rows, err := s.pool.Query(ctx, `SELECT id::text, name FROM creatives WHERE id = ANY($1::text[])`, lit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]string, len(ids))
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[id] = name
	}
	return out, rows.Err()
}

// campaignConversions 按 campaign_id 聚合 ad_conversions 的各 event_type 计数。
func (s *Store) campaignConversions(
	ctx context.Context, campaignID string, from, to time.Time,
) (map[string]int64, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT event_type, COUNT(*)::bigint
		FROM ad_conversions
		WHERE campaign_id = $1 AND ts >= $2 AND ts < $3
		GROUP BY event_type`, campaignID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]int64)
	for rows.Next() {
		var et string
		var n int64
		if err := rows.Scan(&et, &n); err != nil {
			return nil, err
		}
		out[et] = n
	}
	return out, rows.Err()
}

// campaignDaily 按任务时区切分 [from,to) 内每日曝光/点击（补齐连续日，无数据补 0）。
func (s *Store) campaignDaily(
	ctx context.Context, creativeIDs []string, tz string, from, to time.Time,
) ([]DashboardCampaignDaily, error) {
	if len(creativeIDs) == 0 {
		return []DashboardCampaignDaily{}, nil
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}
	// 任务时区下的起止「本地日」边界（绝对时刻），generate_series 据此展开连续日
	localStart := time.Date(from.In(loc).Year(), from.In(loc).Month(), from.In(loc).Day(), 0, 0, 0, 0, loc)
	localEnd := time.Date(to.In(loc).Year(), to.In(loc).Month(), to.In(loc).Day(), 0, 0, 0, 0, loc)
	lit := "{" + strings.Join(creativeIDs, ",") + "}"

	rows, err := s.pool.Query(ctx, `
		SELECT to_char(d.day AT TIME ZONE $4, 'YYYY-MM-DD'),
		       COALESCE(SUM(CASE WHEN ae.event_type = 'impression' THEN 1 ELSE 0 END), 0)::bigint,
		       COALESCE(SUM(CASE WHEN ae.event_type = 'click' THEN 1 ELSE 0 END), 0)::bigint
		FROM generate_series($1::timestamptz, $2::timestamptz, interval '1 day') AS d(day)
		LEFT JOIN ad_events ae
		       ON ae.ts >= d.day AND ae.ts < d.day + interval '1 day'
		      AND ae.event_type IN ('impression', 'click')
		      AND ae.creative_id = ANY($3::bigint[])
		GROUP BY d.day
		ORDER BY d.day`, localStart, localEnd, lit, tz)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]DashboardCampaignDaily, 0)
	for rows.Next() {
		var d DashboardCampaignDaily
		if err := rows.Scan(&d.Date, &d.Impressions, &d.Clicks); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// DashboardAdvertiser 广告主 KPI 监控行（含预警标识）。
type DashboardAdvertiser struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Status       string  `json:"status"`
	DailyBudget  float64 `json:"daily_budget"`
	SpentToday   float64 `json:"spent_today"`
	TargetKPIValue float64 `json:"target_kpi_value"`
	Achievement  float64 `json:"achievement"`
	Warning      string  `json:"warning"` // "" / "paused" / "budget" / "kpi"
}

// DashboardSlot 广告位实时状态行。
type DashboardSlot struct {
	Code          string `json:"code"`
	SlotKey       string `json:"slot_key"`
	Name          string `json:"name"`
	Type          string `json:"type"`
	Status        string `json:"status"`
	AppName       string `json:"app_name"`
	FillCount     int    `json:"fill_count"`      // 启用中的填充优先级条数
	RequestsToday int64  `json:"requests_today"`  // 今日请求
	FillsToday    int64  `json:"fills_today"`     // 今日填充
}

func clampAchievement(target, actual float64) float64 {
	if actual <= 0 || target <= 0 {
		return 1
	}
	return math.Min(math.Max(target/actual, 0.25), 4.0)
}

// DashboardOverview 拉取看板总览 KPI（今日）。
func (s *Store) DashboardOverview(ctx context.Context) (*DashboardOverview, error) {
	o := &DashboardOverview{}
	var tgt float64
	if err := s.pool.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(daily_budget), 0)::float8,
			COALESCE(SUM(spent_today), 0)::float8,
			CASE WHEN SUM(spent_today) > 0 AND SUM(spent_today / NULLIF(target_kpi_value, 0)) > 0
			     THEN SUM(spent_today) / SUM(spent_today / NULLIF(target_kpi_value, 0)) ELSE 0 END,
			COUNT(*) FILTER (WHERE status = 'active'),
			COUNT(DISTINCT advertiser_id) FILTER (WHERE status = 'active')
		FROM campaigns WHERE deleted_at IS NULL`).
		Scan(&o.DailyBudget, &o.SpentToday, &tgt, &o.ActiveCampaigns, &o.ActiveAdvertisers); err != nil {
		return nil, err
	}
	// 实测 CPI 不再落库，总体达成率暂置中性 1.0（后期引入运行时实测值再计算）。
	o.Achievement = clampAchievement(tgt, 0)

	if err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(requests), 0)::bigint, COALESCE(SUM(fills), 0)::bigint,
		       COALESCE(SUM(impressions), 0)::bigint, COALESCE(SUM(clicks), 0)::bigint,
		       COALESCE(SUM(conversions), 0)::bigint, COALESCE(SUM(revenue), 0)::float8
		FROM metrics_minute WHERE minute_ts >= date_trunc('day', now())`).
		Scan(&o.Requests, &o.Fills, &o.Impressions, &o.Clicks, &o.Conversions, &o.Revenue); err != nil {
		return nil, err
	}
	if o.Requests > 0 {
		o.FillRate = float64(o.Fills) / float64(o.Requests)
	}
	if o.Impressions > 0 {
		o.ECPM = o.Revenue / float64(o.Impressions) * 1000
	}
	return o, nil
}

// DashboardOverviewRange 按日期区间 [from, to) 拉取看板总览 KPI（to 为开区间）。
//
// 与 DashboardOverview（今日）的区别：请求/填充/曝光/点击/消耗按传入区间从
// metrics_minute 聚合，并额外按 event_type 从 ad_conversions 拆出
// 安装/激活/注册/转化/付费 五项。
// 日预算/今日消耗/活跃数取自 campaigns 的「今日」快照（无历史快照可查），不随区间变化。
func (s *Store) DashboardOverviewRange(ctx context.Context, from, to time.Time) (*DashboardOverview, error) {
	o := &DashboardOverview{}
	var tgt float64
	if err := s.pool.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(daily_budget), 0)::float8,
			COALESCE(SUM(spent_today), 0)::float8,
			CASE WHEN SUM(spent_today) > 0 AND SUM(spent_today / NULLIF(target_kpi_value, 0)) > 0
			     THEN SUM(spent_today) / SUM(spent_today / NULLIF(target_kpi_value, 0)) ELSE 0 END,
			COUNT(*) FILTER (WHERE status = 'active'),
			COUNT(DISTINCT advertiser_id) FILTER (WHERE status = 'active')
		FROM campaigns WHERE deleted_at IS NULL`).
		Scan(&o.DailyBudget, &o.SpentToday, &tgt, &o.ActiveCampaigns, &o.ActiveAdvertisers); err != nil {
		return nil, err
	}
	// 实测 CPI 不再落库，总体达成率暂置中性 1.0（后期引入运行时实测值再计算）。
	o.Achievement = clampAchievement(tgt, 0)

	if err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(requests), 0)::bigint, COALESCE(SUM(fills), 0)::bigint,
		       COALESCE(SUM(impressions), 0)::bigint, COALESCE(SUM(clicks), 0)::bigint,
		       COALESCE(SUM(conversions), 0)::bigint, COALESCE(SUM(revenue), 0)::float8
		FROM metrics_minute WHERE minute_ts >= $1 AND minute_ts < $2`, from, to).
		Scan(&o.Requests, &o.Fills, &o.Impressions, &o.Clicks, &o.Conversions, &o.Revenue); err != nil {
		return nil, err
	}
	if o.Requests > 0 {
		o.FillRate = float64(o.Fills) / float64(o.Requests)
	}
	if o.Impressions > 0 {
		o.ECPM = o.Revenue / float64(o.Impressions) * 1000
	}

	// 转化事件拆分：ad_conversions 为 S2S 回传明细，event_type 见 config.ConversionEvents。
	rows, err := s.pool.Query(ctx, `
		SELECT event_type, COUNT(*)::bigint
		FROM ad_conversions
		WHERE ts >= $1 AND ts < $2
		GROUP BY event_type`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var et string
		var n int64
		if err := rows.Scan(&et, &n); err != nil {
			return nil, err
		}
		switch et {
		case "install":
			o.Install = n
		case "activate":
			o.Activate = n
		case "register":
			o.Register = n
		case "first_purchase":
			o.Conversion = n
		case "purchase":
			o.Pay = n
		}
	}
	return o, rows.Err()
}

// DashboardAdvertisers 广告主维度 KPI 监控（按广告主聚合旗下 campaign）。
func (s *Store) DashboardAdvertisers(ctx context.Context) ([]DashboardAdvertiser, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT a.id::text, a.name, a.status,
			COALESCE(SUM(c.daily_budget), 0)::float8,
			COALESCE(SUM(c.spent_today), 0)::float8,
			COALESCE(SUM(c.target_kpi_value), 0)::float8,
			CASE WHEN SUM(c.spent_today) > 0 AND SUM(c.spent_today / NULLIF(c.target_kpi_value, 0)) > 0
			     THEN SUM(c.spent_today) / SUM(c.spent_today / NULLIF(c.target_kpi_value, 0)) ELSE 0 END
		FROM advertisers a
		LEFT JOIN campaigns c ON c.advertiser_id = a.id AND c.deleted_at IS NULL
		WHERE a.deleted_at IS NULL
		GROUP BY a.id, a.name, a.status
		ORDER BY a.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]DashboardAdvertiser, 0)
	for rows.Next() {
		var a DashboardAdvertiser
		var tgt float64
		if err := rows.Scan(&a.ID, &a.Name, &a.Status, &a.DailyBudget, &a.SpentToday,
			&a.TargetKPIValue, &tgt); err != nil {
			return nil, err
		}
		// 实测 CPI 不再落库，达成率暂置中性 1.0（后期引入运行时实测值再计算）。
		a.Achievement = clampAchievement(tgt, 0)
		switch {
		case a.Status != "active":
			a.Warning = "paused"
		case a.DailyBudget > 0 && a.SpentToday >= a.DailyBudget*0.95:
			a.Warning = "budget"
		case a.Achievement < 0.8 || a.Achievement > 1.2:
			a.Warning = "kpi"
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// DashboardSlots 广告位实时状态（含今日请求/填充）。
func (s *Store) DashboardSlots(ctx context.Context) ([]DashboardSlot, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT s.id::text, s.slot_key, s.name, s.type, s.status,
		       COALESCE(app.name, ''),
		       COALESCE((SELECT COUNT(*) FROM fill_priorities fp
		                 WHERE fp.slot_code = s.slot_key AND fp.enabled = true), 0)::int,
		       COALESCE(SUM(m.requests), 0)::bigint,
		       COALESCE(SUM(m.fills), 0)::bigint
		FROM ad_slots s
		JOIN apps app ON app.code = s.app_code
		-- 000022 起指标按「展现样式 style」聚合（metrics_minute.slot_id 已改名为 style），
		-- 广告位监控按 ad_slots.type（=样式）关联当日分钟指标；多广告主同样式会汇总到该样式。
		LEFT JOIN metrics_minute m ON m.style = s.type AND m.minute_ts >= date_trunc('day', now())
		WHERE s.deleted_at IS NULL
		GROUP BY s.id, s.slot_key, s.name, s.type, s.status, app.name
		ORDER BY s.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]DashboardSlot, 0)
	for rows.Next() {
		var sl DashboardSlot
		if err := rows.Scan(&sl.Code, &sl.SlotKey, &sl.Name, &sl.Type, &sl.Status,
			&sl.AppName, &sl.FillCount, &sl.RequestsToday, &sl.FillsToday); err != nil {
			return nil, err
		}
		out = append(out, sl)
	}
	return out, rows.Err()
}


