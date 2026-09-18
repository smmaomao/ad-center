package store

import (
	"context"
	"fmt"
	"time"
)

// ============================================================
// 广告主总钱包（充值 - 扣费）
//
// 余额真相在 DB（advertisers.wallet_balance）：
//   - 充值：后台 Recharge 同事务递增余额 + 写 advertiser_recharges 流水；
//   - 扣费：消费端 WriteEventBatch 与 budget_ledger 同事务递减余额。
// 进程内预算控制器持有余额副本用于**实时只读闸**（余额 <=0 停投），
// 启动/对账时用 LoadWalletBalances 全量对齐，故 DB 与内存最终一致。
//
// 与 campaign 日预算的区别：日预算 spent 只在进程内、DB 不回收写；钱包
// 余额必须落库（是广告主的钱，重启不能"复活"）。
// ============================================================

// LoadWalletBalances 加载**已启用钱包**的广告主余额（wallet_enabled=true）。
// 未启用的广告主不进返回集 → 预算控制器视为"未注册钱包" → 不受总余额闸限制
// （存量广告主向后兼容）。
func (s *Store) LoadWalletBalances(ctx context.Context) (map[string]float64, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, wallet_balance::float8
		FROM advertisers
		WHERE deleted_at IS NULL AND wallet_enabled`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]float64{}
	for rows.Next() {
		var id string
		var bal float64
		if err := rows.Scan(&id, &bal); err != nil {
			return nil, err
		}
		out[id] = bal
	}
	return out, rows.Err()
}

// AdminWallet 广告主钱包概览（后台展示）。
type AdminWallet struct {
	AdvertiserID string  `json:"advertiser_id"`
	Enabled      bool    `json:"enabled"`   // 是否已启用总钱包闸
	Balance      float64 `json:"balance"`   // 当前余额
	Deposited    float64 `json:"deposited"` // 累计充值
	Spent        float64 `json:"spent"`     // 累计扣费
	Currency     string  `json:"currency"`
}

// GetWallet 汇总某广告主钱包：启用状态 + 当前余额 + 累计充值 + 累计扣费。
//
// 累计扣费只取 op_type='deduct_agg'（消费端聚合行，M2 权威来源）：memory 队列下
// ledgerFn 还会写每事件的 'deduct' 行，与聚合行是同一笔钱，同时统计会翻倍。
func (s *Store) GetWallet(ctx context.Context, advertiserID string) (*AdminWallet, error) {
	w := &AdminWallet{AdvertiserID: advertiserID, Currency: "USD"}
	err := s.pool.QueryRow(ctx, `
		SELECT
			COALESCE((SELECT wallet_enabled FROM advertisers WHERE id = $1::bigint AND deleted_at IS NULL), false),
			COALESCE((SELECT wallet_balance::float8 FROM advertisers WHERE id = $1::bigint AND deleted_at IS NULL), 0),
			COALESCE((SELECT sum(amount)::float8 FROM advertiser_recharges WHERE advertiser_id = $1::bigint), 0),
			COALESCE((SELECT sum(amount)::float8 FROM budget_ledger
			           WHERE advertiser_id = $1::bigint AND op_type = 'deduct_agg'), 0)`,
		advertiserID).Scan(&w.Enabled, &w.Balance, &w.Deposited, &w.Spent)
	if err != nil {
		return nil, err
	}
	return w, nil
}

// Recharge 充值入账：同事务写充值流水 + 递增余额 + 置 wallet_enabled=true，
// 保证「流水」与「余额」一致（不会出现记了流水没加钱的半截状态）。
// 返回充值后的新余额。
func (s *Store) Recharge(ctx context.Context, advertiserID string, amount float64, currency, note, actor string) (float64, error) {
	if amount <= 0 {
		return 0, fmt.Errorf("amount must be positive")
	}
	if currency == "" {
		currency = "USD"
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var newBalance float64
	err = tx.QueryRow(ctx, `
		UPDATE advertisers
		   SET wallet_balance = wallet_balance + $2::float8,
		       wallet_enabled = true,
		       updated_at = now()
		 WHERE id = $1::bigint AND deleted_at IS NULL
		 RETURNING wallet_balance::float8`, advertiserID, amount).Scan(&newBalance)
	if err != nil {
		return 0, fmt.Errorf("advertiser not found: %s", advertiserID)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO advertiser_recharges (advertiser_id, amount, currency, note, created_by)
		VALUES ($1::bigint, $2::float8, $3, $4, $5)`,
		advertiserID, amount, currency, nilIfEmpty(note), nilIfEmpty(actor)); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return newBalance, nil
}

// WalletAdjustInput 手动调账入参。
type WalletAdjustInput struct {
	Mode   string  // "set"=把余额改为 Amount；"delta"=在现有余额上增减 Amount
	Amount float64 // 目标余额或增减额（delta 可正可负）
	Note   string
}

// AdjustWallet 手动调账：修正余额并写一条调账流水（amount = 实际变动额，可正可负）。
// 不改变 wallet_enabled（闸门开关由列表页单独控制）；不并入"累计充值"。
// 返回调整后的余额。
func (s *Store) AdjustWallet(ctx context.Context, advertiserID string, in WalletAdjustInput, actor string) (float64, error) {
	if in.Mode != "set" && in.Mode != "delta" {
		return 0, fmt.Errorf("invalid mode: %s", in.Mode)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// 行锁读出当前余额，保证 delta 计算与写入原子（并发调账不丢更新）。
	var old float64
	if err := tx.QueryRow(ctx, `
		SELECT wallet_balance::float8 FROM advertisers
		 WHERE id = $1::bigint AND deleted_at IS NULL
		 FOR UPDATE`, advertiserID).Scan(&old); err != nil {
		return 0, fmt.Errorf("advertiser not found: %s", advertiserID)
	}

	delta := in.Amount
	if in.Mode == "set" {
		delta = in.Amount - old
	}
	if delta == 0 {
		return 0, fmt.Errorf("调整后余额无变化")
	}
	newBalance := old + delta
	if newBalance < 0 {
		return 0, fmt.Errorf("调整后余额不能为负（当前 $%.2f）", old)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE advertisers
		   SET wallet_balance = $2::float8, updated_at = now()
		 WHERE id = $1::bigint AND deleted_at IS NULL`, advertiserID, newBalance); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO advertiser_wallet_adjustments (advertiser_id, amount, note, created_by)
		VALUES ($1::bigint, $2::float8, $3, $4)`,
		advertiserID, delta, nilIfEmpty(in.Note), nilIfEmpty(actor)); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return newBalance, nil
}

// WalletFlowRow 钱包流水一行（充值 + 扣费 + 调账合并视图）。
type WalletFlowRow struct {
	Kind      string  `json:"kind"`   // recharge=充值 / deduct=扣费 / adjust=调账
	Amount    float64 `json:"amount"` // 金额（recharge/deduct 恒为正，方向由 kind 区分；adjust 为带符号变动额）
	Currency  string  `json:"currency"`
	OpType    string  `json:"op_type,omitempty"` // 扣费细分：deduct / deduct_agg
	Note      string  `json:"note,omitempty"`
	CreatedBy string  `json:"created_by,omitempty"`
	At        string  `json:"at"` // YYYY-MM-DD HH24:MI:SS
}

// ListWalletFlow 合并充值（advertiser_recharges）、调账（advertiser_wallet_adjustments）
// 与扣费流水（budget_ledger），按时间倒序。
// 不依赖 budget_ledger.count（部分库该聚合列缺失），只取金额与时间。
func (s *Store) ListWalletFlow(ctx context.Context, advertiserID string, limit int) ([]*WalletFlowRow, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.pool.Query(ctx, `
		SELECT kind, amount, currency, op_type, note, created_by, at FROM (
			SELECT 'recharge'::text AS kind, amount::float8 AS amount, currency,
			       ''::text AS op_type, COALESCE(note, '') AS note,
			       COALESCE(created_by, '') AS created_by, created_at AS at
			FROM advertiser_recharges WHERE advertiser_id = $1::bigint
			UNION ALL
			SELECT 'adjust'::text, amount::float8, 'USD'::text,
			       ''::text, COALESCE(note, ''), COALESCE(created_by, ''),
			       created_at
			FROM advertiser_wallet_adjustments WHERE advertiser_id = $1::bigint
			UNION ALL
			SELECT 'deduct'::text, amount::float8, 'USD'::text, op_type,
			       ''::text, ''::text,
			       day::timestamptz + make_interval(hours => hour_bucket::int)
			FROM budget_ledger
			WHERE advertiser_id = $1::bigint AND op_type = 'deduct_agg'
		) t
		ORDER BY at DESC
		LIMIT $2`, advertiserID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*WalletFlowRow
	for rows.Next() {
		r := &WalletFlowRow{}
		var at time.Time
		if err := rows.Scan(&r.Kind, &r.Amount, &r.Currency, &r.OpType, &r.Note, &r.CreatedBy, &at); err != nil {
			return nil, err
		}
		r.At = at.Format("2006-01-02 15:04:05")
		out = append(out, r)
	}
	return out, rows.Err()
}
