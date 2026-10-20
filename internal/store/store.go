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
	// adminAuth 后台 RBAC 查询（角色 + 菜单树）的进程内短缓存，见 admin_auth.go
	// 末尾「RBAC 查询短缓存」说明。直接构造 Store（不经 New）时为 nil，
	// 缓存方法对 nil 接收者安全降级为不缓存。
	adminAuth *adminAuthCache
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
	// MinConns>0：始终保持至少 2 条物理连接，避免空闲后被回收、首个请求被迫重建
	// 连接（Fly/Upstash 场景下重建要重走 DNS+TLS，首请求会慢 1~2s）。
	cfg.MinConns = 2
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return &Store{pool: pool, adminAuth: newAdminAuthCache()}, nil
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
		CreativesByAdvertiser: map[string][]*config.Creative{},
		CreativesByID:         map[string]*config.Creative{},
		Campaigns:             map[string]*config.Campaign{},
		CreativeCampaign:      map[string]string{},
		Settings:              map[string]json.RawMessage{},
		// 先给兜底标准线，settings 里有配置再覆盖（避免除零）
		PricingBenchmark: config.DefaultPricingBenchmark(),
	}

	rows, err := s.pool.Query(ctx, `
		SELECT code, name, api_key_hash, status, COALESCE(callback_url, ''), COALESCE(ad_watch_params, ''), COALESCE(add_server_id, '')
		FROM apps WHERE status = 'active' AND deleted_at IS NULL`)
	if err != nil {
		return nil, fmt.Errorf("load apps: %w", err)
	}
	for rows.Next() {
		a := &config.App{}
		if err := rows.Scan(&a.ID, &a.Name, &a.APIKeyHash, &a.Status, &a.CallbackURL, &a.AdWatchParams, &a.AddServerID); err != nil {
			rows.Close()
			return nil, err
		}
		snap.Apps[a.ID] = a
		snap.AppByKeyHash[a.APIKeyHash] = a
	}
	rows.Close()

	// 广告主只加载「身份 + 状态 + 定向」；计费相关（出价/计费方式/CPA 单价）
	// 一律在 campaign 级加载——广告主不参与任何排序或扣费计算。
	rows, err = s.pool.Query(ctx, `
		SELECT id::text, name, status, targeting
		FROM advertisers WHERE status = 'active' AND deleted_at IS NULL`)
	if err != nil {
		return nil, fmt.Errorf("load advertisers: %w", err)
	}
	for rows.Next() {
		a := &config.Advertiser{}
		var targeting []byte
		if err := rows.Scan(&a.ID, &a.Name, &a.Status,
			&targeting); err != nil {
			rows.Close()
			return nil, err
		}
		var err error
		if a.Targeting, err = config.ParseTargeting(targeting); err != nil {
			rows.Close()
			return nil, fmt.Errorf("advertiser %s targeting: %w", a.ID, err)
		}
		snap.Advertisers[a.ID] = a
	}
	rows.Close()

	// ad_slots / fill_priorities 已删除（迁移 000068）：广告位/填充优先级是遗留死配置，
	// 真正生效的频控在 campaigns 表（engine 用 camp.Freq* 构造 AdvPolicy 调 CheckAndIncr）。
	// 快照不再加载这两张表（原 LoadSnapshot 的两趟查询已移除）。

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
		snap.CreativesByID[c.ID] = c
	}
	rows.Close()

	// 广告任务（campaign）：预算闸与扣费的执行粒度。构建 creative→campaign 归属映射，
	// 引擎与扣费路径据其把预算单元从 advertiser 收敛到 campaign。
	rows, err = s.pool.Query(ctx, `
		SELECT id::text, advertiser_id::text, name, status,
		       daily_budget::float8, spent_today::float8, consume_speed,
		       target_kpi_type, target_kpi_value::float8,
		       billing_mode, bidding_price::float8, bidding_price_min::float8,
		       cpa_event_prices, priority_score::float8,
		       COALESCE(deliver_ttl_minutes, 10)::int,
		       COALESCE(creative_ids, '{}')::text[], start_at, end_at,
		       COALESCE(landing_url, ''),
		       COALESCE(freq_daily_limit, 0)::int,
		       COALESCE(freq_interval_minutes, 0)::int,
		       COALESCE(freq_fatigue_window, 0)::int,
		       COALESCE(timezone, 'UTC')
		FROM campaigns WHERE status = 'active' AND deleted_at IS NULL`)
	if err != nil {
		return nil, fmt.Errorf("load campaigns: %w", err)
	}
	for rows.Next() {
		c := &config.Campaign{}
		var creativeIDs []string
		var cpaPrices []byte
		if err := rows.Scan(&c.ID, &c.AdvertiserID, &c.Name, &c.Status,
			&c.DailyBudget, &c.SpentToday, &c.ConsumeSpeed,
			&c.TargetKPIType, &c.TargetKPIValue,
			&c.BillingMode, &c.BiddingPrice, &c.BiddingPriceMin, &cpaPrices,
			&c.PriorityScore,
			&c.DeliverTTLMinutes,
			&creativeIDs, &c.StartAt, &c.EndAt, &c.LandingURL,
			&c.FreqDailyLimit, &c.FreqIntervalMinute, &c.FreqFatigueWindow, &c.Timezone); err != nil {
			rows.Close()
			return nil, err
		}
		if c.CPAEventPrices, err = config.ParseCPAEventPrices(cpaPrices); err != nil {
			rows.Close()
			return nil, fmt.Errorf("campaign %s cpa_event_prices: %w", c.ID, err)
		}
		c.CreativeIDs = creativeIDs
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

// ConversionExists 判断 (click_id, event_type) 是否已记过账——S2S 转化幂等去重的
// 依据。同一 clickid 的重放会命中，从而不再重复扣费。
func (s *Store) ConversionExists(ctx context.Context, clickID, eventType string) (bool, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM ads_center.ad_conversions WHERE click_id=$1 AND event_type=$2)`,
		clickID, eventType).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}
