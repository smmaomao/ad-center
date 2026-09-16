// Package store 是 Postgres 访问层（pgx 直连，search_path=ads_center），
// 仅服务启动、配置加载、管理 API、定时落库等非决策路径；决策路径全程内存。
package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"adcenter/internal/config"
)

// Store 持有连接池。决策路径不得引用本包。
type Store struct {
	pool *pgxpool.Pool
}

// Pool 暴露底层连接池，供迁移等基础设施使用。
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

// New 创建连接池（search_path 固定 ads_center）。
func New(ctx context.Context, dsn string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = "ads_center"
	cfg.MaxConns = 8
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return &Store{pool: pool}, nil
}

// Close 关闭连接池。
func (s *Store) Close() { s.pool.Close() }

// ============================================================
// 配置快照加载（ConfigCache 用）
// ============================================================

// LoadSnapshot 全量加载五张配置表为不可变快照。
func (s *Store) LoadSnapshot(ctx context.Context) (*config.Snapshot, error) {
	snap := &config.Snapshot{
		Apps:                  map[string]*config.App{},
		AppByKeyHash:          map[string]*config.App{},
		Advertisers:           map[string]*config.Advertiser{},
		Slots:                 map[string]*config.Slot{},
		SlotsByKey:            map[string]*config.Slot{},
		CreativesByAdvertiser: map[string][]*config.Creative{},
		Campaigns:            map[string]*config.Campaign{},
		CreativeCampaign:     map[string]string{},
		Settings:              map[string]json.RawMessage{},
		// 先给兜底标准线，settings 里有配置再覆盖（避免除零）
		PricingBenchmark: config.DefaultPricingBenchmark(),
	}

	rows, err := s.pool.Query(ctx, `
		SELECT code, name, api_key_hash, status, COALESCE(callback_url, '')
		FROM apps WHERE status = 'active' AND deleted_at IS NULL`)
	if err != nil {
		return nil, fmt.Errorf("load apps: %w", err)
	}
	for rows.Next() {
		a := &config.App{}
		if err := rows.Scan(&a.ID, &a.Name, &a.APIKeyHash, &a.Status, &a.CallbackURL); err != nil {
			rows.Close()
			return nil, err
		}
		snap.Apps[a.ID] = a
		snap.AppByKeyHash[a.APIKeyHash] = a
	}
	rows.Close()

	rows, err = s.pool.Query(ctx, `
		SELECT id::text, name, status,
		       bidding_price::float8, bidding_price_min::float8,
		       billing_mode, cpa_event_prices,
		       targeting, freq_windows
		FROM advertisers WHERE deleted_at IS NULL`)
	if err != nil {
		return nil, fmt.Errorf("load advertisers: %w", err)
	}
	for rows.Next() {
		a := &config.Advertiser{}
		var targeting, freqWindows, cpaPrices []byte
		if err := rows.Scan(&a.ID, &a.Name, &a.Status,
			&a.BiddingPrice, &a.BiddingPriceMin,
			&a.BillingMode, &cpaPrices,
			&targeting, &freqWindows); err != nil {
			rows.Close()
			return nil, err
		}
		var err error
		if a.Targeting, err = config.ParseTargeting(targeting); err != nil {
			rows.Close()
			return nil, fmt.Errorf("advertiser %s targeting: %w", a.ID, err)
		}
		if a.FreqWindows, err = config.ParseFreqWindows(freqWindows); err != nil {
			rows.Close()
			return nil, fmt.Errorf("advertiser %s freq_windows: %w", a.ID, err)
		}
		if a.CPAEventPrices, err = config.ParseCPAEventPrices(cpaPrices); err != nil {
			rows.Close()
			return nil, fmt.Errorf("advertiser %s cpa_event_prices: %w", a.ID, err)
		}
		snap.Advertisers[a.ID] = a
	}
	rows.Close()

	rows, err = s.pool.Query(ctx, `
		SELECT id::text, app_code::text, slot_key, name, type, status,
		       freq_daily_limit, freq_interval_minutes, freq_fatigue_window
		FROM ad_slots`)
	if err != nil {
		return nil, fmt.Errorf("load ad_slots: %w", err)
	}
	for rows.Next() {
		sl := &config.Slot{}
		if err := rows.Scan(&sl.ID, &sl.AppID, &sl.Key, &sl.Name, &sl.Type, &sl.Status,
			&sl.FreqDailyLimit, &sl.FreqIntervalMinutes, &sl.FreqFatigueWindow); err != nil {
			rows.Close()
			return nil, err
		}
		snap.Slots[sl.ID] = sl
		snap.SlotsByKey[sl.Key] = sl
	}
	rows.Close()

	rows, err = s.pool.Query(ctx, `
		SELECT id::text, slot_code::text, source_type,
		       COALESCE(advertiser_id::text, ''),
		       expected_ecpm::float8, guaranteed_share::float8, weight::float8
		FROM fill_priorities WHERE enabled ORDER BY slot_code, position`)
	if err != nil {
		return nil, fmt.Errorf("load fill_priorities: %w", err)
	}
	for rows.Next() {
		fp := config.FillPriority{}
		var slotID string
		if err := rows.Scan(&fp.ID, &slotID, &fp.SourceType, &fp.AdvertiserID,
			&fp.ExpectedECPM, &fp.GuaranteedShare, &fp.Weight); err != nil {
			rows.Close()
			return nil, err
		}
		if sl, ok := snap.Slots[slotID]; ok {
			sl.Priorities = append(sl.Priorities, fp)
		}
	}
	rows.Close()

	rows, err = s.pool.Query(ctx, `
		SELECT id::text, COALESCE(advertiser_id::text, ''), name, media_type, storage_path,
		       orientation, COALESCE(width, 0), COALESCE(height, 0), COALESCE(duration_ms, 0),
		       status, COALESCE(ab_group, ''),
		       COALESCE(styles, '{}'), COALESCE(target_apps, '{}')::text[]
		FROM creatives WHERE status IN ('active', 'testing') AND deleted_at IS NULL`)
	if err != nil {
		return nil, fmt.Errorf("load creatives: %w", err)
	}
	for rows.Next() {
		c := &config.Creative{}
		if err := rows.Scan(&c.ID, &c.AdvertiserID, &c.Name, &c.MediaType, &c.StoragePath,
			&c.Orientation, &c.Width, &c.Height, &c.DurationMS, &c.Status, &c.ABGroup,
			&c.Styles, &c.TargetApps); err != nil {
			rows.Close()
			return nil, err
		}
		snap.CreativesByAdvertiser[c.AdvertiserID] = append(snap.CreativesByAdvertiser[c.AdvertiserID], c)
	}
	rows.Close()

	// 广告任务（campaign）：预算闸与扣费的执行粒度。构建 creative→campaign 归属映射，
	// 引擎与扣费路径据其把预算单元从 advertiser 收敛到 campaign。
	rows, err = s.pool.Query(ctx, `
		SELECT id::text, advertiser_id::text, name, status,
		       daily_budget::float8, spent_today::float8, consume_speed,
		       target_cpi::float8, actual_cpi::float8,
		       billing_mode, bidding_price::float8, priority_score::float8, bidding_mode,
		       COALESCE(deliver_ttl_minutes, 10)::int,
		       COALESCE(creative_ids, '{}')::text[], start_at, end_at,
		       COALESCE(landing_url, ''),
		       COALESCE(freq_daily_limit, 0)::int,
		       COALESCE(freq_interval_minutes, 0)::int,
		       COALESCE(freq_fatigue_window, 0)::int
		FROM campaigns WHERE deleted_at IS NULL`)
	if err != nil {
		return nil, fmt.Errorf("load campaigns: %w", err)
	}
	for rows.Next() {
		c := &config.Campaign{}
		var creativeIDs []string
		if err := rows.Scan(&c.ID, &c.AdvertiserID, &c.Name, &c.Status,
			&c.DailyBudget, &c.SpentToday, &c.ConsumeSpeed,
			&c.TargetCPI, &c.ActualCPI,
			&c.BillingMode, &c.BiddingPrice, &c.PriorityScore, &c.BiddingMode,
			&c.DeliverTTLMinutes,
			&creativeIDs, &c.StartAt, &c.EndAt, &c.LandingURL,
			&c.FreqDailyLimit, &c.FreqIntervalMinute, &c.FreqFatigueWindow); err != nil {
			rows.Close()
			return nil, err
		}
		snap.Campaigns[c.ID] = c
		for _, crID := range creativeIDs {
			// 多 campaign 引用同一素材时，首个映射生效（创意归属唯一）。
			if _, exists := snap.CreativeCampaign[crID]; !exists {
				snap.CreativeCampaign[crID] = c.ID
			}
		}
	}
	rows.Close()

	// 运行时设置（决策缓存等后台可配置项）
	rows, err = s.pool.Query(ctx, `SELECT code, value FROM settings`)
	if err != nil {
		return nil, fmt.Errorf("load settings: %w", err)
	}
	for rows.Next() {
		var k string
		var v []byte
		if err := rows.Scan(&k, &v); err != nil {
			rows.Close()
			return nil, err
		}
		snap.Settings[k] = v
	}
	rows.Close()

	// 平台计费标准线（后台「系统设置」配置，key = pricing_benchmark）。
	// 改了之后配置快照热更新即生效，打分基准分随之变化，无需重启。
	if raw, ok := snap.Settings["pricing_benchmark"]; ok {
		var b config.PricingBenchmark
		if err := json.Unmarshal(raw, &b); err == nil {
			snap.PricingBenchmark = &b
		}
	}

	return snap, nil
}

// ============================================================
// 运行时设置（settings 表，决策缓存等后台可配置项）
// ============================================================

// GetSetting 读取某设置项的原始 JSON 值。未找到返回 (nil, pgx.ErrNoRows)。
func (s *Store) GetSetting(ctx context.Context, key string) (json.RawMessage, error) {
	var v []byte
	if err := s.pool.QueryRow(ctx, `SELECT value FROM settings WHERE code = $1`, key).Scan(&v); err != nil {
		return nil, err
	}
	return v, nil
}

// UpsertSetting 写入/更新设置项（触发 notify_config_changed → 配置快照热更新）。
func (s *Store) UpsertSetting(ctx context.Context, key string, value json.RawMessage) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO settings (code, value) VALUES ($1, $2)
		ON CONFLICT (id) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`,
		key, value)
	return err
}
