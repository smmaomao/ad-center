-- 开发数据重置：清空并重造 广告主 / 产品 / 广告任务 / 素材。
--
-- 因外键依赖，TRUNCATE 会级联清空 decision_logs / ad_events / fill_priorities
-- （决策/事件埋点 与 广告位填充配置，均依赖这些实体；fill_priorities 属 slot 范畴，
--  与「先不管 slot」一致）。apps / ad_slots / roles / admin_users 不受影响。
--
-- 用法: psql "$DATABASE_URL" -f scripts/reseed_entities.sql

-- 1) 级联清空（RESTART IDENTITY 重置四表自增序列，便于下方显式 id 从 1 起）
TRUNCATE TABLE ads_center.advertisers RESTART IDENTITY CASCADE;

-- 2) 广告主（2 个）
INSERT INTO ads_center.advertisers (id, name, status, billing_mode, bidding_price, cpa_event_prices)
VALUES
  (1, 'UrgentGame', 'active', 'cpa', 2.0, '{"install":[2.0,2.0]}'::jsonb),
  (2, 'SteadyShop', 'active', 'cpa', 1.5, '{"install":[1.5,1.5]}'::jsonb);

-- 3) 产品（挂在广告主下）
INSERT INTO ads_center.products (id, advertiser_id, name, status, daily_budget)
VALUES
  (1, 1, '极速狂飙', 'active', 1000),
  (2, 2, '优选商城', 'active', 800);

-- 4) 素材（storage_path 为占位对象 key；真实媒体请在后台上传以获取真实 R2 key）
INSERT INTO ads_center.creatives
  (id, advertiser_id, name, media_type, storage_path, file_size_bytes, status, weight,
   orientation, styles, target_apps, variants, billing_mode, price, impressions, clicks, conversions,
   width, height, duration_ms)
VALUES
  (1, 1, '极速狂飙-视频A', 'video',
   'creatives/1111111111111111111111111111111111111111111111111111111111111111.mp4',
   5242880, 'active', 1.0, 'portrait', '{}', '{}', '[]', 'cpa', 2.0, 0, 0, 0, 720, 1280, 15000),
  (2, 1, '极速狂飙-图片B', 'image',
   'creatives/2222222222222222222222222222222222222222222222222222222222222222.png',
   1048576, 'active', 1.0, 'portrait', '{}', '{}', '[]', 'cpa', 2.0, 0, 0, 0, 720, 1280, NULL),
  (3, 2, '优选商城-视频C', 'video',
   'creatives/3333333333333333333333333333333333333333333333333333333333333333.mp4',
   4194304, 'active', 1.0, 'square', '{}', '{}', '[]', 'cpa', 1.5, 0, 0, 0, 1080, 1080, 20000);

-- 5) 广告任务（关联产品 + 素材 + 落地页）
INSERT INTO ads_center.campaigns
  (id, advertiser_id, product_id, name, status, bidding_mode, bidding_price, bidding_price_min,
   billing_mode, cpa_event_prices, target_cpi, daily_budget, freq_daily_limit, freq_interval_minutes,
   freq_fatigue_window, creative_ids, actual_cpi, spent_today, consume_speed, guaranteed_enabled,
   guaranteed_min_share, priority_score, deliver_ttl_minutes, landing_url)
VALUES
  (1, 1, 1, '极速狂飙-冲量', 'active', 'cpa', 2.0, 1.5, 'cpa',
   '{"install":[2.0,2.0]}'::jsonb, 2.0, 500, 8, 20, 3, '{1,2}'::text[], 0, 0, 'even',
   false, 0, 0, 10, 'https://lp.example.com/jisukuangbiao'),
  (2, 2, 2, '优选商城-拉新', 'active', 'cpa', 1.5, 1.0, 'cpa',
   '{"install":[1.5,1.5]}'::jsonb, 1.5, 400, 8, 20, 3, '{3}'::text[], 0, 0, 'even',
   false, 0, 0, 10, 'https://lp.example.com/youxuan');

-- 6) 推进序列，避免后续 UI 自动插入与本次显式 id 冲突
SELECT setval('ads_center.advertisers_id_seq', (SELECT max(id) FROM ads_center.advertisers));
SELECT setval('ads_center.products_id_seq',    (SELECT max(id) FROM ads_center.products));
SELECT setval('ads_center.campaigns_id_seq',   (SELECT max(id) FROM ads_center.campaigns));
SELECT setval('ads_center.creatives_id_seq',   (SELECT max(id) FROM ads_center.creatives));

-- 7) 触发配置快照重载（notify_config_changed 是触发器函数，需用 pg_notify 模拟）
--    监听端按 payload 表名做定向重载；这里把四张表都通知一遍，确保运行中服务读到新数据。
SELECT pg_notify('ads_center_config_changed', 'advertisers'),
       pg_notify('ads_center_config_changed', 'products'),
       pg_notify('ads_center_config_changed', 'campaigns'),
       pg_notify('ads_center_config_changed', 'creatives');
