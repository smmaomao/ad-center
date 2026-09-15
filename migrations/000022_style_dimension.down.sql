-- 回滚 000022：style(TEXT) 还原为 slot_id(UUID, FK→ad_slots)。
-- 注意：已写入的样式字符串（如 'rewarded_video'）无法转回合法 UUID，回滚会失败；
-- 本 down 仅用于「尚未写入新数据」的紧急回退场景。

ALTER TABLE clicks RENAME COLUMN style TO slot_id;

ALTER TABLE metrics_minute DROP CONSTRAINT IF EXISTS metrics_minute_pkey;
ALTER TABLE metrics_minute RENAME COLUMN style TO slot_id;
ALTER TABLE metrics_minute ALTER COLUMN slot_id TYPE UUID USING slot_id::uuid;
ALTER TABLE metrics_minute ADD PRIMARY KEY (slot_id, advertiser_id, minute_ts);
ALTER TABLE metrics_minute ADD CONSTRAINT metrics_minute_slot_id_fkey
  FOREIGN KEY (slot_id) REFERENCES ads_center.ad_slots(slot_id) ON DELETE CASCADE;

ALTER TABLE ad_events RENAME COLUMN style TO slot_id;
ALTER TABLE ad_events ALTER COLUMN slot_id TYPE UUID USING slot_id::uuid;
ALTER TABLE ad_events ADD CONSTRAINT ad_events_slot_id_fkey
  FOREIGN KEY (slot_id) REFERENCES ads_center.ad_slots(slot_id);
DROP INDEX IF EXISTS idx_ad_events_style_ts;
CREATE INDEX idx_ad_events_slot_ts ON ads_center.ad_events (slot_id, ts);
