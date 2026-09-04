package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ============================================================
// 运行态落库（MetricsAgg / 事件 / 预算流水，全部异步批量）
// ============================================================

// MinuteMetric 一行分钟级指标（advertiserID 传零值 UUID 表示兜底/MAX）。
type MinuteMetric struct {
	SlotID, AdvertiserID, AppID string
	Minute                       time.Time
	Requests, Fills, Impressions, Clicks, Conversions int64
	Revenue                      float64
}

// FlushMinuteMetrics 批量 UPSERT 分钟指标（ON CONFLICT 累加，多实例/重试安全）。
func (s *Store) FlushMinuteMetrics(ctx context.Context, rows []MinuteMetric) error {
	if len(rows) == 0 {
		return nil
	}
	var sb strings.Builder
	sb.WriteString(`INSERT INTO metrics_minute
		(slot_id, advertiser_id, app_id, minute_ts, requests, fills, impressions, clicks, conversions, revenue)
		VALUES `)
	args := make([]any, 0, len(rows)*10)
	for i, r := range rows {
		if i > 0 {
			sb.WriteByte(',')
		}
		fmt.Fprintf(&sb, "($%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d)",
			i*10+1, i*10+2, i*10+3, i*10+4, i*10+5, i*10+6, i*10+7, i*10+8, i*10+9, i*10+10)
		args = append(args, r.SlotID, r.AdvertiserID, r.AppID, r.Minute,
			r.Requests, r.Fills, r.Impressions, r.Clicks, r.Conversions, r.Revenue)
	}
	sb.WriteString(` ON CONFLICT (slot_id, advertiser_id, minute_ts) DO UPDATE SET
		requests = metrics_minute.requests + EXCLUDED.requests,
		fills = metrics_minute.fills + EXCLUDED.fills,
		impressions = metrics_minute.impressions + EXCLUDED.impressions,
		clicks = metrics_minute.clicks + EXCLUDED.clicks,
		conversions = metrics_minute.conversions + EXCLUDED.conversions,
		revenue = metrics_minute.revenue + EXCLUDED.revenue`)
	_, err := s.pool.Exec(ctx, sb.String(), args...)
	return err
}

// AdEvent 原始事件行。
type AdEvent struct {
	AppID, SlotID, AdvertiserID, CreativeID, DeviceID, Country, EventType string
	Revenue float64
}

// InsertAdEvents 批量写事件（fill/impression/click/conversion 回执与下发记录）。
func (s *Store) InsertAdEvents(ctx context.Context, events []AdEvent) error {
	if len(events) == 0 {
		return nil
	}
	var sb strings.Builder
	sb.WriteString(`INSERT INTO ad_events
		(app_id, slot_id, advertiser_id, creative_id, device_id, country, event_type, revenue)
		VALUES `)
	args := make([]any, 0, len(events)*8)
	for i, e := range events {
		if i > 0 {
			sb.WriteByte(',')
		}
		fmt.Fprintf(&sb, "($%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d)",
			i*8+1, i*8+2, i*8+3, i*8+4, i*8+5, i*8+6, i*8+7, i*8+8)
		args = append(args, e.AppID, e.SlotID,
			nilIfEmpty(e.AdvertiserID), nilIfEmpty(e.CreativeID),
			e.DeviceID, nilIfEmpty(e.Country), e.EventType, e.Revenue)
	}
	_, err := s.pool.Exec(ctx, sb.String(), args...)
	return err
}

// WriteLedger 记预算流水（deduct/commit/rollback/calibrate/daily_reset）。
func (s *Store) WriteLedger(ctx context.Context, advertiserID, appID, opType string, amount float64) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO budget_ledger (advertiser_id, app_id, op_type, amount, day, hour_bucket)
		VALUES ($1, $2, $3, $4, CURRENT_DATE, extract(hour FROM now())::smallint)`,
		advertiserID, nilIfEmpty(appID), opType, amount)
	return err
}

// BudgetBalance 预算余额（启动时加载，之后进程内维护，ledger 对账）。
type BudgetBalance struct {
	AdvertiserID string
	DailyBudget  float64
	SpentToday   float64
}

// LoadBudgetBalances 加载未删除广告主的日预算与今日已耗（budget 每日以 DB 为真相起点）。
func (s *Store) LoadBudgetBalances(ctx context.Context) ([]BudgetBalance, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT advertiser_id::text, daily_budget::float8, spent_today::float8
		FROM advertisers WHERE deleted_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BudgetBalance
	for rows.Next() {
		var b BudgetBalance
		if err := rows.Scan(&b.AdvertiserID, &b.DailyBudget, &b.SpentToday); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// GetAdminRole 查后台用户角色（管理 API RBAC 校验，actor 由 BFF 传入）。
func (s *Store) GetAdminRole(ctx context.Context, email string) (string, error) {
	var role string
	err := s.pool.QueryRow(ctx,
		`SELECT role FROM admin_users WHERE email = $1`, email).Scan(&role)
	if errors.Is(err, context.Canceled) {
		return "", err
	}
	if err != nil {
		return "", fmt.Errorf("actor not in admin_users: %s", email)
	}
	return role, nil
}

// WriteAudit 管理操作审计。
func (s *Store) WriteAudit(ctx context.Context, actor, action, entityType, entityID string, diff []byte) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO audit_logs (actor_email, action, entity_type, entity_id, diff)
		VALUES ($1, $2, $3, $4, $5)`, actor, action, entityType, entityID, diff)
	return err
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
