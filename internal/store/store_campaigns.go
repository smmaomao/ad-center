package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ============================================================
// 广告任务（Campaign）CRUD
// campaign 是 KPI / 预算的执行粒度（advertiser → product → campaign）：
// 出价 / 目标 CPI / 日预算 / 保量 / 消耗节奏 等运行期 KPI 都落在 campaign 上，
// 产品（product）与广告主（advertiser）只做归集层（汇总展示）。
// 决策引擎当前仍以 advertiser 为总预算闸（account 级 daily_budget），
// 后续可下沉到 campaign 粒度（见 CampaignRollup）。
// ============================================================

// AdminCampaign 广告任务管理视图。
type AdminCampaign struct {
	ID                 string                `json:"id"`
	AdvertiserID       string                `json:"advertiser_id"`
	AdvertiserName     string                `json:"advertiser_name"`
	Name               string                `json:"name"`
	Status             string                `json:"status"`
	BiddingPrice       float64               `json:"bidding_price"`
	BiddingPriceMin    float64               `json:"bidding_price_min"`
	BillingMode        string                `json:"billing_mode"`
	CPAEventPrices     map[string][2]float64 `json:"cpa_event_prices"`
	TargetKPIType      string                `json:"target_kpi_type"`
	TargetKPIValue     float64               `json:"target_kpi_value"`
	DailyBudget        float64               `json:"daily_budget"`
	SpentToday         float64               `json:"spent_today"`
	ConsumeSpeed       int                   `json:"consume_speed"`
	DeliverTTLMinutes  int                   `json:"deliver_ttl_minutes"`
	GuaranteedEnabled  bool                  `json:"guaranteed_enabled"`
	GuaranteedMinShare float64               `json:"guaranteed_min_share"`
	PriorityScore      float64               `json:"priority_score"`
	FreqDailyLimit     int                   `json:"freq_daily_limit"`
	FreqIntervalMinute int                   `json:"freq_interval_minutes"`
	FreqFatigueWindow  int                   `json:"freq_fatigue_window"`
	CreativeIDs        []string              `json:"creative_ids"`
	ProductID          string                `json:"product_id"`
	ProductName        string                `json:"product_name"`
	StartAt            *string               `json:"start_at"`
	EndAt              *string               `json:"end_at"`
	LandingURL         string                `json:"landing_url"`
}

const campaignCols = `
	c.id::text, c.advertiser_id::text, a.name, c.name, c.status,
	c.bidding_price::float8, c.bidding_price_min::float8,
	c.billing_mode, c.cpa_event_prices::text, c.target_kpi_type, c.target_kpi_value::float8,
	c.daily_budget::float8,
	c.spent_today::float8, c.consume_speed,
	COALESCE(c.deliver_ttl_minutes, 10)::int,
	c.guaranteed_enabled, c.guaranteed_min_share::float8, c.priority_score::float8,
	c.freq_daily_limit, c.freq_interval_minutes,
	c.freq_fatigue_window, COALESCE(c.creative_ids, '{}')::text[],
	COALESCE(c.product_id::text, ''), COALESCE(p.name, ''),
	c.start_at::text, c.end_at::text, COALESCE(c.landing_url, '')`

func scanCampaign(scan func(...any) error) (*AdminCampaign, error) {
	c := &AdminCampaign{}
	var cpaPrices []byte
	if err := scan(&c.ID, &c.AdvertiserID, &c.AdvertiserName, &c.Name, &c.Status,
		&c.BiddingPrice, &c.BiddingPriceMin,
		&c.BillingMode, &cpaPrices, &c.TargetKPIType, &c.TargetKPIValue,
		&c.DailyBudget, &c.SpentToday, &c.ConsumeSpeed,
		&c.DeliverTTLMinutes,
		&c.GuaranteedEnabled, &c.GuaranteedMinShare, &c.PriorityScore,
		&c.FreqDailyLimit, &c.FreqIntervalMinute, &c.FreqFatigueWindow,
		&c.CreativeIDs, &c.ProductID, &c.ProductName, &c.StartAt, &c.EndAt, &c.LandingURL); err != nil {
		return nil, err
	}
	if len(cpaPrices) > 0 {
		if err := json.Unmarshal(cpaPrices, &c.CPAEventPrices); err != nil {
			return nil, err
		}
	}
	return c, nil
}

// ListCampaigns 广告任务列表（未删除，按创建时间倒序）。
func (s *Store) ListCampaigns(ctx context.Context) ([]*AdminCampaign, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+campaignCols+`
		FROM campaigns c JOIN advertisers a ON a.id = c.advertiser_id
		LEFT JOIN products p ON p.id = c.product_id AND p.deleted_at IS NULL
		WHERE c.deleted_at IS NULL
		ORDER BY c.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*AdminCampaign
	for rows.Next() {
		c, err := scanCampaign(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetCampaign 单个广告任务详情。
func (s *Store) GetCampaign(ctx context.Context, id string) (*AdminCampaign, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT `+campaignCols+`
		FROM campaigns c JOIN advertisers a ON a.id = c.advertiser_id
		LEFT JOIN products p ON p.id = c.product_id AND p.deleted_at IS NULL
		WHERE c.id = $1::bigint AND c.deleted_at IS NULL`, id)
	return scanCampaign(row.Scan)
}

var campaignWritable = map[string]bool{
	"advertiser_id": true, "name": true, "status": true,
	"bidding_price": true, "bidding_price_min": true,
	"billing_mode": true, "cpa_event_prices": true, "target_kpi_type": true, "target_kpi_value": true,
	"daily_budget": true, "spent_today": true,
	"consume_speed": true, "deliver_ttl_minutes": true, "guaranteed_enabled": true, "guaranteed_min_share": true,
	"priority_score": true, "freq_daily_limit": true, "freq_interval_minutes": true,
	"freq_fatigue_window": true, "creative_ids": true,
	"start_at": true, "end_at": true, "product_id": true, "landing_url": true, "created_by": true, "updated_by": true,
}

// campaignIntCols 取值需以 bigint 绑定的列（由 uuid 迁移而来的 id 列）。
// 注意 creative_ids 仍是 text[]（迁移未改），不在此列。
var campaignIntCols = map[string]bool{"advertiser_id": true, "product_id": true}

// normalizeCampaignArrays 把 JSON 解码出的 []any 转成 []string（creative_ids）。
func normalizeCampaignArrays(fields map[string]any) {
	v, ok := fields["creative_ids"]
	if !ok || v == nil {
		return
	}
	switch t := v.(type) {
	case []string:
		fields["creative_ids"] = t
	case []any:
		out := make([]string, 0, len(t))
		for _, x := range t {
			if s, ok := x.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		fields["creative_ids"] = out
	default:
		delete(fields, "creative_ids")
	}
}

// CreateCampaign 新建广告任务。
func (s *Store) CreateCampaign(ctx context.Context, fields map[string]any) (string, error) {
	// 若仅提供 product_id，先反查其所属广告主（多产品广告主场景）
	if pid, ok := fields["product_id"].(string); ok && pid != "" {
		adv, err := s.ProductAdvertiser(ctx, pid)
		if err != nil {
			return "", err
		}
		fields["advertiser_id"] = adv
	}
	for _, k := range []string{"advertiser_id", "name"} {
		if v, ok := fields[k]; !ok || v == "" {
			return "", fmt.Errorf("%s required", k)
		}
	}
	normalizeCampaignArrays(fields)
	if err := normalizeJSONFields(fields, "cpa_event_prices"); err != nil {
		return "", err
	}
	cols, placeholders, args := buildInsert(fields, campaignWritable, campaignIntCols)
	var id string
	err := s.pool.QueryRow(ctx, fmt.Sprintf(
		`INSERT INTO campaigns (%s) VALUES (%s) RETURNING id::text`,
		cols, placeholders), args...).Scan(&id)
	return id, err
}

var campaignUpdatable = map[string]bool{
	"name": true, "status": true,
	"bidding_price": true, "bidding_price_min": true,
	"billing_mode": true, "cpa_event_prices": true, "target_kpi_type": true, "target_kpi_value": true,
	"daily_budget": true, "spent_today": true,
	"consume_speed": true, "deliver_ttl_minutes": true, "guaranteed_enabled": true, "guaranteed_min_share": true,
	"priority_score": true, "freq_daily_limit": true, "freq_interval_minutes": true,
	"freq_fatigue_window": true, "creative_ids": true,
	"start_at": true, "end_at": true, "product_id": true, "landing_url": true, "updated_by": true,
}

// UpdateCampaign 部分更新（白名单列）。
func (s *Store) UpdateCampaign(ctx context.Context, id string, fields map[string]any) error {
	normalizeCampaignArrays(fields)
	if pid, ok := fields["product_id"].(string); ok && pid != "" {
		adv, err := s.ProductAdvertiser(ctx, pid)
		if err != nil {
			return err
		}
		fields["advertiser_id"] = adv
	}
	if err := normalizeJSONFields(fields, "cpa_event_prices"); err != nil {
		return err
	}
	sets, args := buildUpdate(fields, campaignUpdatable, campaignIntCols)
	if len(sets) == 0 {
		return fmt.Errorf("no writable fields")
	}
	args = append(args, id)
	tag, err := s.pool.Exec(ctx, fmt.Sprintf(
		`UPDATE campaigns SET %s, updated_at = now()
		 WHERE id = $%d::bigint AND deleted_at IS NULL`,
		strings.Join(sets, ", "), len(args)), args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("campaign not found: %s", id)
	}
	return nil
}

// SoftDeleteCampaign 软删广告任务。
func (s *Store) SoftDeleteCampaign(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE campaigns SET deleted_at = now() WHERE id = $1::bigint AND deleted_at IS NULL`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("campaign not found: %s", id)
	}
	return nil
}

// ============================================================
// 归集查询（campaign 为执行粒度，product / advertiser 为归集层）
// ============================================================

// CampaignRollup 按广告主 / 产品归集的 KPI 汇总。
// 达成率口径与 adminAdvertiserCols 保持一致（target/actual、actual≤0 取 1、clamp[0.25,4]）。
type CampaignRollup struct {
	CampaignCount int     `json:"campaign_count"`
	DailyBudget   float64 `json:"daily_budget"`         // 各 campaign 日预算之和
	SpentToday    float64 `json:"spent_today"`          // 今日消耗之和
	TargetKPIValue float64 `json:"target_kpi_value"`    // 按花费加权的目标 KPI 值
	Achievement   float64 `json:"achievement"`          // 汇总达成率
	GuaranteedMin float64 `json:"guaranteed_min_share"` // 旗下最大保量份额
}

// RollupAdvertiserKPIs 广告主维度 KPI 汇总（旗下所有 campaign）。
func (s *Store) RollupAdvertiserKPIs(ctx context.Context, advertiserID string) (*CampaignRollup, error) {
	return s.campaignRollup(ctx, "advertiser_id = $1::bigint", advertiserID)
}

// RollupProductKPIs 产品维度 KPI 汇总（该产品下所有 campaign）。
func (s *Store) RollupProductKPIs(ctx context.Context, productID string) (*CampaignRollup, error) {
	return s.campaignRollup(ctx, "product_id = $1::bigint", productID)
}

func (s *Store) campaignRollup(ctx context.Context, where, arg string) (*CampaignRollup, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT COUNT(*),
		       COALESCE(SUM(daily_budget), 0)::float8,
		       COALESCE(SUM(spent_today), 0)::float8,
		       CASE WHEN SUM(spent_today) > 0 AND SUM(spent_today / NULLIF(target_kpi_value, 0)) > 0
		            THEN SUM(spent_today) / SUM(spent_today / NULLIF(target_kpi_value, 0))
		            ELSE 0 END,
		       COALESCE(MAX(guaranteed_min_share), 0)::float8
		FROM campaigns WHERE deleted_at IS NULL AND `+where, arg)
	r := &CampaignRollup{}
	var cnt int
	if err := row.Scan(&cnt, &r.DailyBudget, &r.SpentToday, &r.TargetKPIValue, &r.GuaranteedMin); err != nil {
		return nil, err
	}
	r.CampaignCount = cnt
	// 实测 CPI 不再落库，汇总达成率暂置中性 1.0（后期引入运行时实测值再计算）。
	r.Achievement = 1
	return r, nil
}
