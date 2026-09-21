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
	Style, AdvertiserID, AppID                        string
	Minute                                            time.Time
	Requests, Fills, Impressions, Clicks, Conversions int64
	Revenue                                           float64
}

// FlushMinuteMetrics 批量 UPSERT 分钟指标（ON CONFLICT 累加，多实例/重试安全）。
func (s *Store) FlushMinuteMetrics(ctx context.Context, rows []MinuteMetric) error {
	if len(rows) == 0 {
		return nil
	}
	var sb strings.Builder
	sb.WriteString(`INSERT INTO metrics_minute
		(style, advertiser_id, app_code, minute_ts, requests, fills, impressions, clicks, conversions, revenue)
		VALUES `)
	args := make([]any, 0, len(rows)*10)
	for i, r := range rows {
		if i > 0 {
			sb.WriteByte(',')
		}
		fmt.Fprintf(&sb, "($%d,$%d::bigint,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d)",
			i*10+1, i*10+2, i*10+3, i*10+4, i*10+5, i*10+6, i*10+7, i*10+8, i*10+9, i*10+10)
		args = append(args, r.Style, nilIfEmpty(r.AdvertiserID), r.AppID, r.Minute,
			r.Requests, r.Fills, r.Impressions, r.Clicks, r.Conversions, r.Revenue)
	}
	sb.WriteString(` ON CONFLICT (style, advertiser_id, minute_ts) DO UPDATE SET
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
	AppID, Style, AdvertiserID, CreativeID, DeviceID, Country, EventType string
	CampaignID, PixelID                                                  string // 任务标识：服务端归因任务 / 中介回传 pixel（S2S 转化落库，便于统计对账）
	Revenue                                                              float64
	// CallbackOK 仅 video_complete 事件有意义：是否成功转发到 App 业务后端
	// S2S 回调（true=HTTP 2xx；false=网络错误/非 2xx/未配置 callback_url）。
	// 其他事件为 nil（不记录）。
	CallbackOK *bool
}

// InsertAdEvents 批量写事件（fill/impression/click/conversion 回执与下发记录）。
func (s *Store) InsertAdEvents(ctx context.Context, events []AdEvent) error {
	q, args, ok := buildInsertAdEvents(events)
	if !ok {
		return nil
	}
	_, err := s.pool.Exec(ctx, q, args...)
	return err
}

// WriteEventBatch 在**一个事务**内写入「事件明细 + 聚合扣费流水」（M2）。
//
// 为什么必须同事务：消费是"至少一次"语义，处理失败会重新投递。若两步分开写，
// 失败重投会出现"明细写了、流水没写"的半截状态——重投后明细重复、流水缺失，
// 对账永远对不齐。同事务后要么都成功，要么整批重试。
func (s *Store) WriteEventBatch(ctx context.Context, events []AdEvent) error {
	if len(events) == 0 {
		return nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }() // 已 Commit 时 Rollback 无害

	if q, args, ok := buildInsertAdEvents(events); ok {
		if _, err := tx.Exec(ctx, q, args...); err != nil {
			return err
		}
	}
	if rows := AggregateLedger(events); len(rows) > 0 {
		if q, args, ok := buildUpsertLedgerAgg(rows); ok {
			if _, err := tx.Exec(ctx, q, args...); err != nil {
				return err
			}
		}
		// 广告主总钱包同事务递减：余额真相在 DB，进程内 WalletDeduct 只是实时
		// 只读闸。与流水同事务保证「扣了钱就有流水、有流水就扣了钱」。
		if q, args, ok := buildWalletDebit(rows); ok {
			if _, err := tx.Exec(ctx, q, args...); err != nil {
				return err
			}
		}
	}
	return tx.Commit(ctx)
}

// buildWalletDebit 由聚合流水构造广告主钱包批量递减语句。
// GREATEST(...,0) 兜底防负（正常路径 WalletDeduct 已保证余额充足）。
// 只对已启用钱包的广告主扣减（wallet_enabled）——存量广告主不受影响。
func buildWalletDebit(rows []LedgerAgg) (string, []any, bool) {
	if len(rows) == 0 {
		return "", nil, false
	}
	var sb strings.Builder
	sb.WriteString(`UPDATE advertisers a
		SET wallet_balance = GREATEST(a.wallet_balance - v.amt, 0), updated_at = now()
		FROM (VALUES `)
	args := make([]any, 0, len(rows)*2)
	for i, r := range rows {
		if i > 0 {
			sb.WriteByte(',')
		}
		fmt.Fprintf(&sb, "($%d::bigint,$%d::float8)", i*2+1, i*2+2)
		args = append(args, nilIfEmpty(r.AdvertiserID), r.Amount)
	}
	sb.WriteString(`) AS v(id, amt) WHERE a.id = v.id AND a.wallet_enabled`)
	return sb.String(), args, true
}

func buildInsertAdEvents(events []AdEvent) (string, []any, bool) {
	if len(events) == 0 {
		return "", nil, false
	}
	var sb strings.Builder
	sb.WriteString(`INSERT INTO ad_events
		(app_code, style, advertiser_id, creative_id, device_id, country, event_type, revenue, callback_ok, campaign_id, pixel_id)
		VALUES `)
	args := make([]any, 0, len(events)*11)
	for i, e := range events {
		if i > 0 {
			sb.WriteByte(',')
		}
		fmt.Fprintf(&sb, "($%d,$%d,$%d::bigint,$%d::bigint,$%d,$%d,$%d,$%d,$%d,$%d,$%d)",
			i*11+1, i*11+2, i*11+3, i*11+4, i*11+5, i*11+6, i*11+7, i*11+8, i*11+9, i*11+10, i*11+11)
		args = append(args, e.AppID, e.Style,
			nilIfEmpty(e.AdvertiserID), nilIfEmpty(e.CreativeID),
			e.DeviceID, nilIfEmpty(e.Country), e.EventType, e.Revenue, e.CallbackOK,
			e.CampaignID, e.PixelID)
	}
	return sb.String(), args, true
}

// WriteLedger 记预算流水（V1.2：deduct=事件确认扣费/calibrate/daily_reset；
// commit/rollback 为 V1.1 预扣模型遗留，不再产生）。
func (s *Store) WriteLedger(ctx context.Context, advertiserID, appID, opType string, amount float64) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO budget_ledger (advertiser_id, app_code, op_type, amount, day, hour_bucket)
		VALUES ($1::bigint, $2, $3, $4, CURRENT_DATE, extract(hour FROM now())::smallint)`,
		advertiserID, nilIfEmpty(appID), opType, amount)
	return err
}

// LedgerAgg 一条聚合扣费流水（广告主 × 当前小时）。
//
// M2（SCALING.md §2）：扣费流水不再每事件一行，而是由消费端在批次内先聚合
// 成 (广告主, 小时) 一行，DB 写入量降 2~3 个数量级。
type LedgerAgg struct {
	AdvertiserID string
	AppID        string // 聚合行跨 app，通常留空
	Amount       float64
	Count        int
}

// WriteLedgerAgg 批量 UPSERT 聚合扣费流水（migration 000013）。
//
// 依赖部分唯一索引 (advertiser_id, day, hour_bucket) WHERE op_type='deduct_agg'：
// 同一小时重复写入走 DO UPDATE 累加，不会产生多行、也不会覆盖历史。
//
// 注意：ON CONFLICT 累加保证的是"不产生重复行"，**不等于**重复投递时金额不重复
// 累加——真正的事件级幂等（event_uid 唯一键）是 M3 待办（SCALING.md §8）。
func (s *Store) WriteLedgerAgg(ctx context.Context, rows []LedgerAgg) error {
	q, args, ok := buildUpsertLedgerAgg(rows)
	if !ok {
		return nil
	}
	_, err := s.pool.Exec(ctx, q, args...)
	return err
}

// AggregateLedger 把批次内的扣费金额聚合成 (广告主, app) 行。
// 只统计 Revenue>0 且归属到广告主的事件——过程性事件（fill / 未归因转化）不产生流水。
func AggregateLedger(events []AdEvent) []LedgerAgg {
	type aggKey struct{ adv, app string }
	agg := make(map[aggKey]*LedgerAgg)
	for _, e := range events {
		if e.Revenue <= 0 || e.AdvertiserID == "" {
			continue
		}
		k := aggKey{e.AdvertiserID, e.AppID}
		a, ok := agg[k]
		if !ok {
			a = &LedgerAgg{AdvertiserID: e.AdvertiserID, AppID: e.AppID}
			agg[k] = a
		}
		a.Amount += e.Revenue
		a.Count++
	}
	rows := make([]LedgerAgg, 0, len(agg))
	for _, a := range agg {
		rows = append(rows, *a)
	}
	return rows
}

func buildUpsertLedgerAgg(rows []LedgerAgg) (string, []any, bool) {
	if len(rows) == 0 {
		return "", nil, false
	}
	var sb strings.Builder
	sb.WriteString(`INSERT INTO budget_ledger
		(advertiser_id, app_code, op_type, amount, count, day, hour_bucket) VALUES `)
	args := make([]any, 0, len(rows)*4)
	for i, r := range rows {
		if i > 0 {
			sb.WriteByte(',')
		}
		fmt.Fprintf(&sb, "($%d::bigint,$%d,'deduct_agg',$%d,$%d,CURRENT_DATE,extract(hour FROM now())::smallint)",
			i*4+1, i*4+2, i*4+3, i*4+4)
		args = append(args, nilIfEmpty(r.AdvertiserID), nilIfEmpty(r.AppID), r.Amount, r.Count)
	}
	sb.WriteString(` ON CONFLICT (advertiser_id, day, hour_bucket) WHERE op_type = 'deduct_agg'
		DO UPDATE SET amount = budget_ledger.amount + EXCLUDED.amount,
		              count = budget_ledger.count + EXCLUDED.count`)
	return sb.String(), args, true
}

// BudgetBalance 预算余额（启动时加载，之后进程内维护，ledger 对账）。
// 预算单元现为 campaign：AdvertiserID 改称 CampaignID。
type BudgetBalance struct {
	CampaignID  string
	DailyBudget float64
	SpentToday  float64
}

// LoadBudgetBalances 加载未删除广告任务的日预算与今日已耗（budget 每日以 DB 为真相起点）。
// 预算闸按 campaign 各自控制，故此处以 campaigns 为数据源。
func (s *Store) LoadBudgetBalances(ctx context.Context) ([]BudgetBalance, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, daily_budget::float8, spent_today::float8
		FROM campaigns WHERE deleted_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BudgetBalance
	for rows.Next() {
		var b BudgetBalance
		if err := rows.Scan(&b.CampaignID, &b.DailyBudget, &b.SpentToday); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// GetAdminRole 查后台用户角色（管理 API RBAC 校验，actor 由 BFF 传入）。
func (s *Store) GetAdminRole(ctx context.Context, email string) (string, error) {
	var role string
	// 与 AuthenticateAdmin 保持一致：邮箱大小写不敏感，避免"登录能过、
	// 但 RBAC 校验（看板 SSE 等）因大小写不一致查不到账号而 403"的坑。
	err := s.pool.QueryRow(ctx,
		`SELECT role_code FROM admin_users WHERE lower(email) = lower($1)`, email).Scan(&role)
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
