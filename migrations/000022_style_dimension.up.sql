-- 去广告位（slot）改造：运行时决策维度从「广告位 slot_id(UUID 外键)」改为
-- 「展现样式 style(TEXT)」。引擎不再依赖 ad_slots / fill_priorities，候选直接是素材。
--
-- 涉及写入路径的三张表：ad_events / metrics_minute / clicks。
-- ad_slots / fill_priorities 表保留（后台广告位管理暂不动），但运行时不再读取。

-- 1) ad_events：slot_id(UUID, FK→ad_slots) → style(TEXT 展现样式)
ALTER TABLE ad_events DROP CONSTRAINT IF EXISTS ad_events_slot_id_fkey;
ALTER TABLE ad_events ALTER COLUMN slot_id TYPE TEXT USING slot_id::text;
ALTER TABLE ad_events RENAME COLUMN slot_id TO style;
DROP INDEX IF EXISTS idx_ad_events_slot_ts;
CREATE INDEX idx_ad_events_style_ts ON ads_center.ad_events (style, ts);

-- 2) metrics_minute：slot_id(UUID, FK→ad_slots, PK 首列) → style(TEXT)，重建主键
ALTER TABLE metrics_minute DROP CONSTRAINT IF EXISTS metrics_minute_slot_id_fkey;
ALTER TABLE metrics_minute DROP CONSTRAINT IF EXISTS metrics_minute_pkey;
ALTER TABLE metrics_minute ALTER COLUMN slot_id TYPE TEXT USING slot_id::text;
ALTER TABLE metrics_minute RENAME COLUMN slot_id TO style;
ALTER TABLE metrics_minute ADD PRIMARY KEY (style, advertiser_id, minute_ts);

-- 3) clicks：slot_id 已是 TEXT，仅重命名（去掉潜在的 FK）
ALTER TABLE clicks DROP CONSTRAINT IF EXISTS clicks_slot_id_fkey;
ALTER TABLE clicks RENAME COLUMN slot_id TO style;
