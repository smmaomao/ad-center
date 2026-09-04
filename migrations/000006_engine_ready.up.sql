-- V1.1：决策引擎就绪所需的表结构补齐 + ConfigCache 通知触发器

-- ============================================================
-- 1. ConfigCache LISTEN/NOTIFY 触发器（配置秒级生效的写端）
--    通道: ads_center_config_changed，payload: 表名
-- ============================================================
CREATE OR REPLACE FUNCTION ads_center.notify_config_changed()
RETURNS TRIGGER AS $$
BEGIN
    PERFORM pg_notify('ads_center_config_changed', TG_TABLE_NAME);
    RETURN NEW;
END $$ LANGUAGE plpgsql;

CREATE TRIGGER trg_apps_notify
    AFTER INSERT OR UPDATE OR DELETE ON ads_center.apps
    FOR EACH ROW EXECUTE FUNCTION ads_center.notify_config_changed();

CREATE TRIGGER trg_advertisers_notify
    AFTER INSERT OR UPDATE OR DELETE ON ads_center.advertisers
    FOR EACH ROW EXECUTE FUNCTION ads_center.notify_config_changed();

CREATE TRIGGER trg_ad_slots_notify
    AFTER INSERT OR UPDATE OR DELETE ON ads_center.ad_slots
    FOR EACH ROW EXECUTE FUNCTION ads_center.notify_config_changed();

CREATE TRIGGER trg_fill_priorities_notify
    AFTER INSERT OR UPDATE OR DELETE ON ads_center.fill_priorities
    FOR EACH ROW EXECUTE FUNCTION ads_center.notify_config_changed();

CREATE TRIGGER trg_creatives_notify
    AFTER INSERT OR UPDATE OR DELETE ON ads_center.creatives
    FOR EACH ROW EXECUTE FUNCTION ads_center.notify_config_changed();

-- ============================================================
-- 2. 广告位客户端标识：slot_key（App 客户端引用广告位的稳定字符串，
--    如 "drama_splash"；客户端不感知 UUID）
-- ============================================================
ALTER TABLE ads_center.ad_slots
    ADD COLUMN slot_key TEXT NOT NULL;
CREATE UNIQUE INDEX uq_ad_slots_key ON ads_center.ad_slots (slot_key);
COMMENT ON COLUMN ads_center.ad_slots.slot_key IS '客户端引用的稳定标识（全局唯一），决策 API 入参';

-- ============================================================
-- 3. 素材播放规格（客户端播放 UI 需要：尺寸/时长）
-- ============================================================
ALTER TABLE ads_center.creatives
    ADD COLUMN width INT,
    ADD COLUMN height INT,
    ADD COLUMN duration_ms INT;
COMMENT ON COLUMN ads_center.creatives.duration_ms IS '视频时长（毫秒），激励视频倒计时 UI 用';

-- ============================================================
-- 4. 修复 metrics_minute 主键缺陷：PK 列不允许 NULL，
--    原设计"advertiser_id NULL = 兜底/MAX"与 PRIMARY KEY 冲突。
--    改用零值 UUID 哨兵表示无广告主（表当前为空，直接改列定义）
-- ============================================================
ALTER TABLE ads_center.metrics_minute DROP CONSTRAINT metrics_minute_pkey;
ALTER TABLE ads_center.metrics_minute
    ALTER COLUMN advertiser_id SET DEFAULT '00000000-0000-0000-0000-000000000000'::uuid;
ALTER TABLE ads_center.metrics_minute
    ADD CONSTRAINT metrics_minute_pkey PRIMARY KEY (slot_id, advertiser_id, minute_ts);
COMMENT ON COLUMN ads_center.metrics_minute.advertiser_id IS '零值 UUID = 兜底/MAX 聚合（PK 列不可 NULL，用哨兵）';
