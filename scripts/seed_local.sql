-- 本地开发种子数据（幂等，可重复执行）
-- 配套 API Key（仅本地）: adc_local_e2e_9f2e7a1c
-- 用法: psql "$DATABASE_URL" -f scripts/seed_local.sql

BEGIN;

-- 后台操作者（本地无 Supabase Auth，直接映射 super_admin）
INSERT INTO ads_center.admin_users (id, email, role)
VALUES ('00000000-0000-0000-0000-000000000001', 'dev@adcenter.local', 'super_admin')
ON CONFLICT (id) DO UPDATE SET role = EXCLUDED.role;

-- 客户端 App（短剧 App）
INSERT INTO ads_center.apps (id, name, api_key_hash, status)
VALUES ('00000000-0000-0000-0000-0000000000a1', '短剧App（本地）',
        '4741ef9f6982de05c7d204b16f3d04b66bbabc05af92746b3202ab45baa2ea92', 'active')
ON CONFLICT (id) DO UPDATE SET api_key_hash = EXCLUDED.api_key_hash, status = 'active';

-- 广告主 ×2：仅承载身份 + 计费锚点。KPI（target/actual CPI）、消耗节奏、投放结束日期、
-- 下发有效期均在各自「广告任务」中配置（见下方 campaigns 种子）。计费方式 cpa → 按 S2S 转化回执扣费。
INSERT INTO ads_center.advertisers
    (advertiser_id, name, status, billing_mode, bidding_price, cpa_event_prices, freq_windows)
VALUES
    ('00000000-0000-0000-0000-0000000000b1', 'UrgentGame', 'active', 'cpa', 2.0,
     '{"install":[2.0,2.0]}'::jsonb,
     '[{"window_minutes":180,"max":3},{"window_minutes":1440,"max":10}]'::jsonb),
    ('00000000-0000-0000-0000-0000000000b2', 'SteadyShop', 'active', 'cpa', 1.5,
     '{"install":[1.5,1.5]}'::jsonb,
     '[{"window_minutes":180,"max":3},{"window_minutes":1440,"max":10}]'::jsonb)
ON CONFLICT (advertiser_id) DO UPDATE SET
    status = 'active', billing_mode = EXCLUDED.billing_mode,
    cpa_event_prices = EXCLUDED.cpa_event_prices;

-- 广告位：短剧开屏（竖屏为主）
INSERT INTO ads_center.ad_slots
    (id, app_id, slot_key, name, type, status,
     freq_daily_limit, freq_interval_minutes, freq_fatigue_window)
VALUES ('00000000-0000-0000-0000-0000000000c1', '00000000-0000-0000-0000-0000000000a1',
        'drama_splash', '短剧开屏', 'splash', 'active', 8, 20, 3)
ON CONFLICT (id) DO UPDATE SET
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

-- 不再写入任何伪造素材：真实素材应通过后台上传流程创建，列表只展示用户真实上传的条目。
-- 下面这条 DELETE 用于清理历史上种子写入的假素材，保证重跑种子后本地列表干净。
DELETE FROM ads_center.creatives
WHERE creative_id IN (
    '00000000-0000-0000-0000-0000000000d1',
    '00000000-0000-0000-0000-0000000000d2'
);

-- 清理历史上种子写入的演示广告任务（与上面的假素材一并移除，避免列表出现无对应素材的孤立任务）。
DELETE FROM ads_center.campaigns
WHERE campaign_id IN (
    '00000000-0000-0000-0000-0000000000c1',
    '00000000-0000-0000-0000-0000000000c2'
);

COMMIT;
