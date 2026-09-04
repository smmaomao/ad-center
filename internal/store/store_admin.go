package store

import (
	"context"
	"fmt"
	"strings"
)

// ============================================================
// 管理 API 数据访问（Next.js BFF 调用，写操作触发 NOTIFY 自动刷新缓存）
// ============================================================

// AdminAdvertiser 管理 API 的广告主视图（含计算字段）。
type AdminAdvertiser struct {
	ID                 string  `json:"id"`
	Name               string  `json:"name"`
	Tier               int     `json:"tier"`
	Status             string  `json:"status"`
	TargetCPI          float64 `json:"target_cpi"`
	ActualCPI          float64 `json:"actual_cpi"`
	Achievement        float64 `json:"achievement"` // actual/target，由查询计算
	DailyBudget        float64 `json:"daily_budget"`
	SpentToday         float64 `json:"spent_today"`
	ConsumeSpeed       string  `json:"consume_speed"`
	BiddingMode        string  `json:"bidding_mode"`
	BiddingPrice       float64 `json:"bidding_price"`
	GuaranteedEnabled  bool    `json:"guaranteed_enabled"`
	GuaranteedMinShare float64 `json:"guaranteed_min_share"`
	EndAt              *string `json:"end_at"`
	Contact            string  `json:"contact"`
}

const adminAdvertiserCols = `
	advertiser_id::text, name, tier, status,
	target_cpi::float8, actual_cpi::float8,
	CASE WHEN target_cpi > 0 THEN actual_cpi / target_cpi ELSE 1 END,
	daily_budget::float8, spent_today::float8, consume_speed,
	bidding_mode, bidding_price::float8,
	guaranteed_enabled, guaranteed_min_share::float8, end_at::text, COALESCE(contact, '')`

func scanAdminAdvertiser(scan func(...any) error) (*AdminAdvertiser, error) {
	a := &AdminAdvertiser{}
	if err := scan(&a.ID, &a.Name, &a.Tier, &a.Status,
		&a.TargetCPI, &a.ActualCPI, &a.Achievement,
		&a.DailyBudget, &a.SpentToday, &a.ConsumeSpeed,
		&a.BiddingMode, &a.BiddingPrice,
		&a.GuaranteedEnabled, &a.GuaranteedMinShare, &a.EndAt, &a.Contact); err != nil {
		return nil, err
	}
	return a, nil
}

// ListAdvertisers 广告主列表（未删除，按 tier/创建时间）。
func (s *Store) ListAdvertisers(ctx context.Context) ([]*AdminAdvertiser, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+adminAdvertiserCols+`
		FROM advertisers WHERE deleted_at IS NULL
		ORDER BY tier, created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*AdminAdvertiser
	for rows.Next() {
		a, err := scanAdminAdvertiser(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// GetAdvertiser 单个广告主详情。
func (s *Store) GetAdvertiser(ctx context.Context, id string) (*AdminAdvertiser, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT `+adminAdvertiserCols+`
		FROM advertisers WHERE advertiser_id = $1 AND deleted_at IS NULL`, id)
	return scanAdminAdvertiser(row.Scan)
}

// advertiserCols 管理端可写列（更新白名单，防注入）。
var advertiserWritable = map[string]bool{
	"name": true, "tier": true, "status": true, "target_cpi": true,
	"daily_budget": true, "consume_speed": true, "bidding_mode": true,
	"bidding_price": true, "guaranteed_enabled": true, "guaranteed_min_share": true,
	"end_at": true, "contact": true, "updated_by": true,
}

// CreateAdvertiser 新建广告主。
func (s *Store) CreateAdvertiser(ctx context.Context, fields map[string]any) (string, error) {
	if _, ok := fields["name"]; !ok {
		return "", fmt.Errorf("name required")
	}
	cols, placeholders, args := buildInsert(fields, advertiserWritable)
	var id string
	err := s.pool.QueryRow(ctx, fmt.Sprintf(
		`INSERT INTO advertisers (%s) VALUES (%s) RETURNING advertiser_id::text`,
		cols, placeholders), args...).Scan(&id)
	return id, err
}

// UpdateAdvertiser 部分更新（白名单列）。
func (s *Store) UpdateAdvertiser(ctx context.Context, id string, fields map[string]any) error {
	sets, args := buildUpdate(fields, advertiserWritable)
	if len(sets) == 0 {
		return fmt.Errorf("no writable fields")
	}
	args = append(args, id)
	tag, err := s.pool.Exec(ctx, fmt.Sprintf(
		`UPDATE advertisers SET %s WHERE advertiser_id = $%d AND deleted_at IS NULL`,
		strings.Join(sets, ", "), len(args)), args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("advertiser not found: %s", id)
	}
	return nil
}

// SoftDeleteAdvertiser 软删（保留审计轨迹）。
func (s *Store) SoftDeleteAdvertiser(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE advertisers SET deleted_at = now() WHERE advertiser_id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("advertiser not found: %s", id)
	}
	return nil
}

// AdminSlot 广告位管理视图。
type AdminSlot struct {
	ID                  string `json:"id"`
	AppID               string `json:"app_id"`
	AppName             string `json:"app_name"`
	Key                 string `json:"key"`
	Name                string `json:"name"`
	Type                string `json:"type"`
	Status              string `json:"status"`
	FreqDailyLimit      int    `json:"freq_daily_limit"`
	FreqIntervalMinutes int    `json:"freq_interval_minutes"`
	FreqFatigueWindow   int    `json:"freq_fatigue_window"`
	FillCount           int    `json:"fill_count"` // 启用的填充来源数
	AIAgentEnabled      bool   `json:"ai_agent_enabled"`
	AIAgentGoal         string `json:"ai_agent_goal"`
}

const slotCols = `
	s.slot_id::text, s.app_id::text, a.name, s.slot_key, s.name, s.type, s.status,
	s.freq_daily_limit, s.freq_interval_minutes, s.freq_fatigue_window, %s
	s.ai_agent_enabled, s.ai_agent_goal`

// ListSlots 广告位列表（带 App 名与填充来源计数）。
func (s *Store) ListSlots(ctx context.Context) ([]*AdminSlot, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+fmt.Sprintf(slotCols, `
		       (SELECT count(*) FROM fill_priorities f WHERE f.slot_id = s.slot_id AND f.enabled),`)+`
		FROM ad_slots s JOIN apps a ON a.app_id = s.app_id
		WHERE s.deleted_at IS NULL
		ORDER BY a.name, s.created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*AdminSlot
	for rows.Next() {
		sl := &AdminSlot{}
		if err := rows.Scan(&sl.ID, &sl.AppID, &sl.AppName, &sl.Key, &sl.Name, &sl.Type, &sl.Status,
			&sl.FreqDailyLimit, &sl.FreqIntervalMinutes, &sl.FreqFatigueWindow, &sl.FillCount,
			&sl.AIAgentEnabled, &sl.AIAgentGoal); err != nil {
			return nil, err
		}
		out = append(out, sl)
	}
	return out, rows.Err()
}

// AdminApp App 管理视图（不含 key 哈希）。
type AdminApp struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	APIKeyPrefix string `json:"api_key_prefix"`
	Status       string `json:"status"`
}

// ListApps App 列表。
func (s *Store) ListApps(ctx context.Context) ([]*AdminApp, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT app_id::text, name, api_key_prefix, status FROM apps ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*AdminApp
	for rows.Next() {
		a := &AdminApp{}
		if err := rows.Scan(&a.ID, &a.Name, &a.APIKeyPrefix, &a.Status); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// CreateApp 注册 App（api key 哈希与展示前缀由调用方生成，原文不落库）。
func (s *Store) CreateApp(ctx context.Context, name, keyPrefix, keyHash string) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx, `
		INSERT INTO apps (name, api_key_prefix, api_key_hash) VALUES ($1, $2, $3)
		RETURNING app_id::text`, name, keyPrefix, keyHash).Scan(&id)
	return id, err
}

// buildInsert 由字段白名单构造 INSERT 片段。
func buildInsert(fields map[string]any, allowed map[string]bool) (cols, placeholders string, args []any) {
	i := 0
	var colList, phList []string
	for k, v := range fields {
		if !allowed[k] {
			continue
		}
		i++
		colList = append(colList, k)
		phList = append(phList, fmt.Sprintf("$%d", i))
		args = append(args, v)
	}
	return strings.Join(colList, ", "), strings.Join(phList, ", "), args
}

// buildUpdate 由字段白名单构造 SET 片段。
func buildUpdate(fields map[string]any, allowed map[string]bool) (sets []string, args []any) {
	for k, v := range fields {
		if !allowed[k] {
			continue
		}
		args = append(args, v)
		sets = append(sets, fmt.Sprintf("%s = $%d", k, len(args)))
	}
	return sets, args
}
