package store

import (
	"context"
	"math"
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
}

// DashboardAdvertiser 广告主 KPI 监控行（含预警标识）。
type DashboardAdvertiser struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Status       string  `json:"status"`
	DailyBudget  float64 `json:"daily_budget"`
	SpentToday   float64 `json:"spent_today"`
	TargetCPI    float64 `json:"target_cpi"`
	ActualCPI    float64 `json:"actual_cpi"`
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
	var tgt, act float64
	if err := s.pool.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(daily_budget), 0)::float8,
			COALESCE(SUM(spent_today), 0)::float8,
			CASE WHEN SUM(spent_today) > 0 AND SUM(spent_today / NULLIF(actual_cpi, 0)) > 0
			     THEN SUM(spent_today) / SUM(spent_today / NULLIF(actual_cpi, 0)) ELSE 0 END,
			CASE WHEN SUM(spent_today) > 0 AND SUM(spent_today / NULLIF(target_cpi, 0)) > 0
			     THEN SUM(spent_today) / SUM(spent_today / NULLIF(target_cpi, 0)) ELSE 0 END,
			COUNT(*) FILTER (WHERE status = 'active'),
			COUNT(DISTINCT advertiser_id) FILTER (WHERE status = 'active')
		FROM campaigns WHERE deleted_at IS NULL`).
		Scan(&o.DailyBudget, &o.SpentToday, &act, &tgt, &o.ActiveCampaigns, &o.ActiveAdvertisers); err != nil {
		return nil, err
	}
	o.Achievement = clampAchievement(tgt, act)

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

// DashboardAdvertisers 广告主维度 KPI 监控（按广告主聚合旗下 campaign）。
func (s *Store) DashboardAdvertisers(ctx context.Context) ([]DashboardAdvertiser, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT a.id::text, a.name, a.status,
			COALESCE(SUM(c.daily_budget), 0)::float8,
			COALESCE(SUM(c.spent_today), 0)::float8,
			COALESCE(SUM(c.target_cpi), 0)::float8,
			COALESCE(SUM(c.actual_cpi), 0)::float8,
			CASE WHEN SUM(c.spent_today) > 0 AND SUM(c.spent_today / NULLIF(c.actual_cpi, 0)) > 0
			     THEN SUM(c.spent_today) / SUM(c.spent_today / NULLIF(c.actual_cpi, 0)) ELSE 0 END,
			CASE WHEN SUM(c.spent_today) > 0 AND SUM(c.spent_today / NULLIF(c.target_cpi, 0)) > 0
			     THEN SUM(c.spent_today) / SUM(c.spent_today / NULLIF(c.target_cpi, 0)) ELSE 0 END
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
		var tgt, act float64
		if err := rows.Scan(&a.ID, &a.Name, &a.Status, &a.DailyBudget, &a.SpentToday,
			&a.TargetCPI, &a.ActualCPI, &act, &tgt); err != nil {
			return nil, err
		}
		a.Achievement = clampAchievement(tgt, act)
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
		       COALESCE(s.fill_count, 0)::int,
		       COALESCE(SUM(m.requests), 0)::bigint,
		       COALESCE(SUM(m.fills), 0)::bigint
		FROM ad_slots s
		JOIN apps app ON app.code = s.app_code
		LEFT JOIN metrics_minute m ON m.slot_id = s.id AND m.minute_ts >= date_trunc('day', now())
		WHERE s.deleted_at IS NULL
		GROUP BY s.id, s.slot_key, s.name, s.type, s.status, app.name, s.fill_count
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

// ============================================================
// 阶段 3.2：事件聚合回写
// 用今日 metrics_minute 回算各 campaign 的 actual_cpi（实际 CPI = 今日消耗 / 今日转化数）。
//
// 数据归属说明（重要）：metrics_minute 以 slot+advertiser 粒度记录，不直接带 campaign_id，
// 因此转化数按「该 campaign 所属广告主的今日转化」近似归集到 campaign。这是已知近似——
// 单广告主多 campaign 时会把转化均摊到各 campaign 的分母，actual_cpi 在同一广告主内趋同。
// 若后续需要 campaign 级精确 CPI，须在事件流/metrics_minute 增加 campaign_id 维度。
// ============================================================

// SyncCampaignKPIs 回写 campaigns.actual_cpi（每约 60s 由后台定时任务调用）。
// spent_today 由预算控制器同步器单独维护，本函数只算 actual_cpi。
func (s *Store) SyncCampaignKPIs(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE campaigns c
		SET actual_cpi = CASE
			WHEN conv.conversions > 0 THEN c.spent_today / conv.conversions
			ELSE 0
		END
		FROM (
			SELECT c2.id AS cid,
			       COALESCE(SUM(mm.conversions), 0)::float8 AS conversions
			FROM campaigns c2
			LEFT JOIN metrics_minute mm
			       ON mm.advertiser_id = c2.advertiser_id
			      AND mm.minute_ts >= date_trunc('day', now())
			WHERE c2.deleted_at IS NULL
			GROUP BY c2.id
		) conv
		WHERE c.id = conv.cid AND c.deleted_at IS NULL`)
	return err
}
