// Package store 是 Postgres 访问层（pgx 直连，search_path=ads_center），
// 仅服务启动、配置加载、管理 API、定时落库等非决策路径；决策路径全程内存。
package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"adcenter/internal/config"
)

// Store 持有连接池。决策路径不得引用本包。
type Store struct {
	pool *pgxpool.Pool
}

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
	}

	rows, err := s.pool.Query(ctx, `
		SELECT app_id::text, name, api_key_prefix, api_key_hash, status
		FROM apps WHERE status = 'active'`)
	if err != nil {
		return nil, fmt.Errorf("load apps: %w", err)
	}
	for rows.Next() {
		a := &config.App{}
		if err := rows.Scan(&a.ID, &a.Name, &a.APIKeyPrefix, &a.APIKeyHash, &a.Status); err != nil {
			rows.Close()
			return nil, err
		}
		snap.Apps[a.ID] = a
		snap.AppByKeyHash[a.APIKeyHash] = a
	}
	rows.Close()

	rows, err = s.pool.Query(ctx, `
		SELECT advertiser_id::text, name, tier, status,
		       target_cpi::float8, actual_cpi::float8, daily_budget::float8,
		       consume_speed, bidding_mode, bidding_price::float8,
		       guaranteed_enabled, guaranteed_min_share::float8,
		       targeting, freq_windows, end_at
		FROM advertisers WHERE deleted_at IS NULL`)
	if err != nil {
		return nil, fmt.Errorf("load advertisers: %w", err)
	}
	for rows.Next() {
		a := &config.Advertiser{}
		var targeting, freqWindows []byte
		if err := rows.Scan(&a.ID, &a.Name, &a.Tier, &a.Status,
			&a.TargetCPI, &a.ActualCPI, &a.DailyBudget,
			&a.ConsumeSpeed, &a.BiddingMode, &a.BiddingPrice,
			&a.GuaranteedEnabled, &a.GuaranteedMinShare,
			&targeting, &freqWindows, &a.EndAt); err != nil {
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
		snap.Advertisers[a.ID] = a
	}
	rows.Close()

	rows, err = s.pool.Query(ctx, `
		SELECT slot_id::text, app_id::text, slot_key, name, type, status,
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
		SELECT id::text, slot_id::text, source_type,
		       COALESCE(advertiser_id::text, ''),
		       expected_ecpm::float8, guaranteed_share::float8, weight::float8
		FROM fill_priorities WHERE enabled ORDER BY slot_id, position`)
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
		SELECT creative_id::text, advertiser_id::text, name, media_type, storage_path,
		       orientation, COALESCE(width, 0), COALESCE(height, 0), COALESCE(duration_ms, 0),
		       status, weight::float8, COALESCE(ab_group, '')
		FROM creatives WHERE status IN ('active', 'testing') AND deleted_at IS NULL`)
	if err != nil {
		return nil, fmt.Errorf("load creatives: %w", err)
	}
	for rows.Next() {
		c := &config.Creative{}
		if err := rows.Scan(&c.ID, &c.AdvertiserID, &c.Name, &c.MediaType, &c.StoragePath,
			&c.Orientation, &c.Width, &c.Height, &c.DurationMS, &c.Status, &c.Weight, &c.ABGroup); err != nil {
			rows.Close()
			return nil, err
		}
		snap.CreativesByAdvertiser[c.AdvertiserID] = append(snap.CreativesByAdvertiser[c.AdvertiserID], c)
	}
	rows.Close()

	return snap, nil
}
