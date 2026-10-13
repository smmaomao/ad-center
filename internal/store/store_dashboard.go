package store

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
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
	Achievement       float64 `json:"achievement"` // 总体达成率（加权）
	Requests          int64   `json:"requests"`    // 今日请求
	Fills             int64   `json:"fills"`       // 今日填充
	Impressions       int64   `json:"impressions"` // 今日曝光
	Clicks            int64   `json:"clicks"`      // 今日点击
	Conversions       int64   `json:"conversions"` // 今日转化
	Revenue           float64 `json:"revenue"`     // 今日消耗金额
	FillRate          float64 `json:"fill_rate"`   // fills/requests
	ECPM              float64 `json:"ecpm"`        // revenue/impressions*1000
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
	Date      string                 `json:"date"` // YYYY-MM-DD
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
	// 素材唯一归属：仅作为 campaign_id 缺失（历史数据）时的回退口径，优先 active。
	_, owned := buildCreativeOwnership(camps)
	evRows, err := s.campaignEventRows(ctx, camps, owned, from, toExclusive)
	if err != nil {
		return nil, err
	}
	// 归集到「任务 -> 该任务时区的本地日」
	byCamp := make(map[int]map[string][2]int64, len(camps))
	for _, r := range evRows {
		m := byCamp[r.ci]
		if m == nil {
			m = make(map[string][2]int64)
			byCamp[r.ci] = m
		}
		v := m[r.day]
		m[r.day] = [2]int64{v[0] + r.imp, v[1] + r.clk}
	}
	stats := make([]DashboardCampaignStat, 0, len(camps))
	for ci, c := range camps {
		st := DashboardCampaignStat{
			CampaignID: c.ID, Name: c.Name, Timezone: c.Timezone, Status: c.Status,
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

		// 趋势按该任务时区的连续本地日补齐；汇总取趋势之和，保证两者口径一致，
		// 且不受查询窗口外扩（±1 天覆盖时区边界）的影响。
		st.Daily = fillDaily(byCamp[ci], c.Timezone, from, to)
		for _, d := range st.Daily {
			st.Impressions += d.Impressions
			st.Clicks += d.Clicks
		}
		stats = append(stats, st)
	}

	days, err := s.dashboardDays(ctx, camps, evRows, from, toExclusive)
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

// dashboardDays 按各任务「本地日」分组：列出当日有活动的广告任务及其关联素材明细。
// 曝光/点击来自 campaignEventRows（优先 ad_events.campaign_id 精确归因，缺失时回退
// 素材唯一归属）；转化按 ad_conversions.campaign_id。
func (s *Store) dashboardDays(
	ctx context.Context, camps []CampaignDashInfo, evRows []evRow, from, toExclusive time.Time,
) ([]DashboardDay, error) {
	if len(camps) == 0 {
		return nil, nil
	}
	campIDs := make([]string, 0, len(camps))
	tzVals := make([]string, 0, len(camps))
	ciByID := make(map[string]int, len(camps))
	for ci, c := range camps {
		ciByID[c.ID] = ci
		campIDs = append(campIDs, c.ID)
		tzVals = append(tzVals, fmt.Sprintf("(%s,%s)", quoteStr(c.ID), quoteTZ(c.Timezone)))
	}
	cte := "WITH tz(cid, tz) AS (VALUES " + strings.Join(tzVals, ",") + ")"
	campLit := "{" + strings.Join(campIDs, ",") + "}"

	qFrom := from.AddDate(0, 0, -1)
	qToEx := toExclusive.AddDate(0, 0, 1)

	// 转化：按 (campaign_id, 本地日, event_type) 归集
	conv := make(map[string]map[string]int64) // key: "<ci>|<day>"
	{
		rows, err := s.pool.Query(ctx, cte+`
			SELECT ac.campaign_id::text, to_char(ac.ts AT TIME ZONE tz.tz, 'YYYY-MM-DD'),
			       ac.event_type, COUNT(*)::bigint
			FROM ad_conversions ac JOIN tz ON tz.cid = ac.campaign_id::text
			WHERE ac.ts >= $1 AND ac.ts < $2 AND ac.campaign_id = ANY($3::text[])
			GROUP BY ac.campaign_id, to_char(ac.ts AT TIME ZONE tz.tz, 'YYYY-MM-DD'), ac.event_type`,
			qFrom, qToEx, campLit)
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
			ci, ok := ciByID[cid]
			if !ok {
				continue
			}
			k := fmt.Sprintf("%d|%s", ci, day)
			if conv[k] == nil {
				conv[k] = make(map[string]int64)
			}
			conv[k][et] += n
		}
		rows.Close()
	}

	type acc struct {
		imp, clk  int64
		conv      map[string]int64
		creatives map[string]*DashboardCreativeDaily
	}
	byDay := make(map[string]map[int]*acc)
	addAcc := func(day string, ci int) *acc {
		if byDay[day] == nil {
			byDay[day] = make(map[int]*acc)
		}
		if byDay[day][ci] == nil {
			byDay[day][ci] = &acc{conv: map[string]int64{}, creatives: map[string]*DashboardCreativeDaily{}}
		}
		return byDay[day][ci]
	}
	for _, r := range evRows {
		a := addAcc(r.day, r.ci)
		a.imp += r.imp
		a.clk += r.clk
		cr := a.creatives[r.cr]
		if cr == nil {
			cr = &DashboardCreativeDaily{CreativeID: r.cr}
			a.creatives[r.cr] = cr
		}
		cr.Impressions += r.imp
		cr.Clicks += r.clk
	}
	for k, m := range conv {
		parts := strings.SplitN(k, "|", 2)
		ci, _ := strconv.Atoi(parts[0])
		day := parts[1]
		a := addAcc(day, ci)
		for et, n := range m {
			a.conv[et] += n
		}
	}

	lo := from.Format("2006-01-02")
	hi := toExclusive.AddDate(0, 0, -1).Format("2006-01-02")
	out := make([]DashboardDay, 0, len(byDay))
	for day, m := range byDay {
		if day < lo || day > hi {
			continue // 丢弃外扩边界日
		}
		dd := DashboardDay{Date: day}
		for ci, c := range camps {
			a := m[ci]
			if a == nil {
				continue
			}
			inst := mapVal(a.conv, "install")
			act := mapVal(a.conv, "activate")
			reg := mapVal(a.conv, "register")
			conv1 := mapVal(a.conv, "first_purchase")
			pay := mapVal(a.conv, "purchase")
			if a.imp == 0 && a.clk == 0 && inst == 0 && act == 0 && reg == 0 && conv1 == 0 && pay == 0 {
				continue
			}
			creatives := make([]DashboardCreativeDaily, 0, len(a.creatives))
			for _, cr := range a.creatives {
				if cr.Impressions == 0 && cr.Clicks == 0 {
					continue
				}
				ctr := 0.0
				if cr.Impressions > 0 {
					ctr = float64(cr.Clicks) / float64(cr.Impressions)
				}
				creatives = append(creatives, DashboardCreativeDaily{
					CreativeID:  cr.CreativeID,
					Name:        c.CreativeNames[cr.CreativeID],
					Impressions: cr.Impressions,
					Clicks:      cr.Clicks,
					Ctr:         ctr,
				})
			}
			dd.Campaigns = append(dd.Campaigns, DashboardCampaignDay{
				CampaignID:  c.ID,
				Name:        c.Name,
				Timezone:    c.Timezone,
				Status:      c.Status,
				Impressions: a.imp,
				Clicks:      a.clk,
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
	sort.Slice(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out, nil
}

func mapVal(m map[string]int64, k string) int64 {
	if m == nil {
		return 0
	}
	return m[k]
}

// CreativeNameMap 批量取素材 id -> 名称（含已停止/非活跃素材，供明细展示）。
// 注意 creatives.id 为 bigint，必须显式 ::text 后再与 text[] 比较，
// 否则报 "operator does not exist: bigint = text"。
func (s *Store) CreativeNameMap(ctx context.Context, ids []string) (map[string]string, error) {
	lit := "{" + strings.Join(ids, ",") + "}"
	rows, err := s.pool.Query(ctx, `SELECT id::text, name FROM creatives WHERE id::text = ANY($1::text[])`, lit)
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

// fillDaily 把「本地日 -> 曝光/点击」补齐为 [from,to] 内连续的本地日序列（无数据补 0）。
func fillDaily(got map[string][2]int64, tz string, from, to time.Time) []DashboardCampaignDaily {
	loc := parseTZ(tz)
	localStart := time.Date(from.In(loc).Year(), from.In(loc).Month(), from.In(loc).Day(), 0, 0, 0, 0, loc)
	localEnd := time.Date(to.In(loc).Year(), to.In(loc).Month(), to.In(loc).Day(), 0, 0, 0, 0, loc)
	out := make([]DashboardCampaignDaily, 0)
	for d := localStart; !d.After(localEnd); d = d.AddDate(0, 0, 1) {
		key := d.Format("2006-01-02")
		v := got[key]
		out = append(out, DashboardCampaignDaily{Date: key, Impressions: v[0], Clicks: v[1]})
	}
	return out
}

// parseTZ 解析任务统计时区：优先按 IANA 名（如 Asia/Bangkok、UTC），否则按固定偏移
// "UTC+7" / "UTC-12" / "UTC+12" 形式解析为 FixedZone。非法值回退 UTC。
func parseTZ(tz string) *time.Location {
	if tz == "" {
		return time.UTC
	}
	if loc, err := time.LoadLocation(tz); err == nil {
		return loc
	}
	s := strings.TrimSpace(tz)
	s = strings.TrimPrefix(s, "UTC")
	s = strings.TrimSpace(s)
	if s == "" {
		return time.UTC
	}
	off, err := strconv.Atoi(s)
	if err != nil || off < -12 || off > 12 {
		return time.UTC
	}
	return time.FixedZone(tz, off*3600)
}

// quoteTZ 把时区字符串安全地拼进 SQL 的 VALUES()（内部数据，仍做单引号转义）。
func quoteTZ(tz string) string {
	if tz == "" {
		return "'UTC'"
	}
	return "'" + strings.ReplaceAll(tz, "'", "''") + "'"
}

// quoteStr 把字符串安全地拼进 SQL 的 VALUES()。
func quoteStr(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// evRow 一行「任务 × 该任务时区的本地日 × 素材」的曝光/点击。
type evRow struct {
	ci  int
	day string // 该任务时区下的本地日 YYYY-MM-DD
	cr  string
	imp int64
	clk int64
}

// campaignEventRows 拉取 [from,to) 内各任务的曝光/点击，按
// (任务, 该任务时区的本地日, 素材) 归集，供汇总 / 趋势 / 按日分组复用（单条 SQL）。
//
// 归因口径：优先 ad_events.campaign_id —— 它是下发/计费时写入的权威任务归属，
// 因此「老任务下线、新任务沿用同批素材」这类素材复用场景也能按任务精确区分；
// campaign_id 缺失（历史数据写入时未带）时回退到素材唯一归属 buildCreativeOwnership，
// 保证这部分数据不丢。
func (s *Store) campaignEventRows(
	ctx context.Context, camps []CampaignDashInfo, owned [][]string, from, to time.Time,
) ([]evRow, error) {
	if len(camps) == 0 {
		return nil, nil
	}
	cte, crList, campIDs := buildAttribCTE(camps, owned)
	ciByID := make(map[string]int, len(camps))
	for ci, c := range camps {
		ciByID[c.ID] = ci
	}
	// 窗口外扩 1 天：覆盖时区边界（本地日可能落在 UTC 窗口之外）
	rows, err := s.pool.Query(ctx, cte+`
		SELECT COALESCE(NULLIF(ae.campaign_id, ''), co.cid) AS cid,
		       to_char(ae.ts AT TIME ZONE COALESCE(tz.tz, co.tz), 'YYYY-MM-DD') AS d,
		       ae.creative_id::text,
		       COUNT(*) FILTER (WHERE ae.event_type = 'impression')::bigint,
		       COUNT(*) FILTER (WHERE ae.event_type = 'click')::bigint
		FROM ad_events ae
		LEFT JOIN co ON co.crid = ae.creative_id::text
		LEFT JOIN tz ON tz.cid = COALESCE(NULLIF(ae.campaign_id, ''), co.cid)
		WHERE ae.ts >= $1 AND ae.ts < $2 AND ae.event_type IN ('impression', 'click')
		      AND (ae.campaign_id = ANY($3::text[]) OR ae.creative_id = ANY($4::bigint[]))
		GROUP BY 1, 2, 3`,
		from.AddDate(0, 0, -1), to.AddDate(0, 0, 1),
		"{"+strings.Join(campIDs, ",")+"}", "{"+strings.Join(crList, ",")+"}")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]evRow, 0)
	for rows.Next() {
		var cid, day, cr string
		var imp, clk int64
		if err := rows.Scan(&cid, &day, &cr, &imp, &clk); err != nil {
			return nil, err
		}
		ci, ok := ciByID[cid]
		if !ok {
			continue // 归属任务不在本次统计范围（已删除/未引用）
		}
		out = append(out, evRow{ci: ci, day: day, cr: cr, imp: imp, clk: clk})
	}
	return out, rows.Err()
}

// buildAttribCTE 构造归因 SQL 的 CTE：
//
//	co(crid, cid, tz) —— 素材 → 唯一归属任务 + 该任务时区（campaign_id 缺失时的回退）
//	tz(cid, tz)       —— 任务 → 统计时区
func buildAttribCTE(camps []CampaignDashInfo, owned [][]string) (cte string, crList, campIDs []string) {
	crVals := make([]string, 0)
	tzVals := make([]string, 0, len(camps))
	campIDs = make([]string, 0, len(camps))
	seen := make(map[string]bool)
	crList = make([]string, 0)
	for ci, c := range camps {
		campIDs = append(campIDs, c.ID)
		tzVals = append(tzVals, fmt.Sprintf("(%s,%s)", quoteStr(c.ID), quoteTZ(c.Timezone)))
		for _, cr := range owned[ci] {
			if seen[cr] {
				continue
			}
			seen[cr] = true
			crList = append(crList, cr)
			crVals = append(crVals, fmt.Sprintf("(%s,%s,%s)", quoteStr(cr), quoteStr(c.ID), quoteTZ(c.Timezone)))
		}
	}
	var coSrc string
	if len(crVals) == 0 {
		coSrc = "SELECT NULL::text, NULL::text, NULL::text WHERE false"
	} else {
		coSrc = "VALUES " + strings.Join(crVals, ",")
	}
	cte = "WITH co(crid, cid, tz) AS (" + coSrc +
		"), tz(cid, tz) AS (VALUES " + strings.Join(tzVals, ",") + ")"
	return
}

// buildCreativeOwnership 决定每个素材唯一归属到哪个任务：
// 优先 active 状态的任务；若被多个任务引用，按 camps 顺序（名称已排序）取第一个，
// 但若遇到 active 任务且当前归属为非 active，则抢占归 active。返回
// ownerOf[creative]=campaign 索引，owned[ci]=该任务归属的素材列表（用于聚合与趋势，
// 避免共用素材被重复统计 / 旧测试任务串到活跃任务数据）。
func buildCreativeOwnership(camps []CampaignDashInfo) (map[string]int, [][]string) {
	ownerOf := make(map[string]int)
	owned := make([][]string, len(camps))
	for ci, c := range camps {
		for _, cr := range c.CreativeIDs {
			cur, exists := ownerOf[cr]
			if !exists {
				ownerOf[cr] = ci
				owned[ci] = append(owned[ci], cr)
				continue
			}
			if c.Status == "active" && camps[cur].Status != "active" {
				ownerOf[cr] = ci
				owned[cur] = removeStr(owned[cur], cr)
				owned[ci] = append(owned[ci], cr)
			}
		}
	}
	return ownerOf, owned
}

func removeStr(s []string, v string) []string {
	out := s[:0]
	for _, x := range s {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}

// DashboardAdvertiser 广告主 KPI 监控行（含预警标识）。
type DashboardAdvertiser struct {
	ID             string  `json:"id"`
	Name           string  `json:"name"`
	Status         string  `json:"status"`
	DailyBudget    float64 `json:"daily_budget"`
	SpentToday     float64 `json:"spent_today"`
	TargetKPIValue float64 `json:"target_kpi_value"`
	Achievement    float64 `json:"achievement"`
	Warning        string  `json:"warning"` // "" / "paused" / "budget" / "kpi"
}

// DashboardSlot 广告位实时状态行。
type DashboardSlot struct {
	Code          string `json:"code"`
	SlotKey       string `json:"slot_key"`
	Name          string `json:"name"`
	Type          string `json:"type"`
	Status        string `json:"status"`
	AppName       string `json:"app_name"`
	FillCount     int    `json:"fill_count"`     // 启用中的填充优先级条数
	RequestsToday int64  `json:"requests_today"` // 今日请求
	FillsToday    int64  `json:"fills_today"`    // 今日填充
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
