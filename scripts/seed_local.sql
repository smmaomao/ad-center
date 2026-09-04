-- 本地开发种子数据（幂等，可重复执行）
-- 配套 API Key（仅本地）: adc_local_e2e_9f2e7a1c
-- 用法: psql "$DATABASE_URL" -f scripts/seed_local.sql

BEGIN;

-- 后台操作者（本地无 Supabase Auth，直接映射 super_admin）
INSERT INTO ads_center.admin_users (auth_user_id, email, role)
VALUES ('00000000-0000-0000-0000-000000000001', 'dev@adcenter.local', 'super_admin')
ON CONFLICT (auth_user_id) DO UPDATE SET role = EXCLUDED.role;

-- 客户端 App（短剧 App）
INSERT INTO ads_center.apps (app_id, name, api_key_prefix, api_key_hash, status)
VALUES ('00000000-0000-0000-0000-0000000000a1', '短剧App（本地）',
        'adc_local_…', '4741ef9f6982de05c7d204b16f3d04b66bbabc05af92746b3202ab45baa2ea92', 'active')
ON CONFLICT (app_id) DO UPDATE SET api_key_hash = EXCLUDED.api_key_hash, status = 'active';

-- 广告主 ×2：urgent 达成率 0.5（KPI 紧急应优先），steady 达成率 1.0
INSERT INTO ads_center.advertisers
    (advertiser_id, name, tier, status, target_cpi, actual_cpi, daily_budget,
     bidding_mode, bidding_price, guaranteed_enabled, guaranteed_min_share,
     freq_windows)
VALUES
    ('00000000-0000-0000-0000-0000000000b1', 'UrgentGame', 1, 'active',
     1.0, 0.5, 100.00, 'cpi', 2.0, TRUE, 0.30,
     '[{"window_minutes":180,"max":3},{"window_minutes":1440,"max":10}]'::jsonb),
    ('00000000-0000-0000-0000-0000000000b2', 'SteadyShop', 2, 'active',
     1.0, 1.0, 80.00, 'cpi', 1.5, FALSE, 0,
     '[{"window_minutes":180,"max":3},{"window_minutes":1440,"max":10}]'::jsonb)
ON CONFLICT (advertiser_id) DO UPDATE SET
    status = 'active', actual_cpi = EXCLUDED.actual_cpi, daily_budget = EXCLUDED.daily_budget;

-- 广告位：短剧开屏（竖屏为主）
INSERT INTO ads_center.ad_slots
    (slot_id, app_id, slot_key, name, type, status,
     freq_daily_limit, freq_interval_minutes, freq_fatigue_window)
VALUES ('00000000-0000-0000-0000-0000000000c1', '00000000-0000-0000-0000-0000000000a1',
        'drama_splash', '短剧开屏', 'splash', 'active', 8, 20, 3)
ON CONFLICT (slot_id) DO UPDATE SET
    slot_key = EXCLUDED.slot_key, status = 'active', app_id = EXCLUDED.app_id;

-- 填充优先级（urgent 保底 30%）
DELETE FROM ads_center.fill_priorities
WHERE slot_id = '00000000-0000-0000-0000-0000000000c1';
INSERT INTO ads_center.fill_priorities
    (slot_id, source_type, advertiser_id, expected_ecpm, guaranteed_share, weight, position)
VALUES
    ('00000000-0000-0000-0000-0000000000c1', 'advertiser',
     '00000000-0000-0000-0000-0000000000b1', 12.0, 0.30, 1.0, 0),
    ('00000000-0000-0000-0000-0000000000c1', 'advertiser',
     '00000000-0000-0000-0000-0000000000b2', 9.0, 0, 1.0, 1);

-- 素材 ×2（竖屏激励视频）
INSERT INTO ads_center.creatives
    (creative_id, advertiser_id, name, media_type, storage_path,
     orientation, width, height, duration_ms, status, weight)
VALUES
    ('00000000-0000-0000-0000-0000000000d1', '00000000-0000-0000-0000-0000000000b1',
     'UrgentGame-竖屏视频A', 'video', 'creatives/00/0b1/d1.mp4',
     'portrait', 1080, 1920, 15000, 'active', 1.0),
    ('00000000-0000-0000-0000-0000000000d2', '00000000-0000-0000-0000-0000000000b2',
     'SteadyShop-竖屏视频A', 'video', 'creatives/00/0b2/d2.mp4',
     'portrait', 1080, 1920, 15000, 'active', 1.0)
ON CONFLICT (creative_id) DO UPDATE SET status = 'active';

COMMIT;
