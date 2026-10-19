-- #4 事件级幂等：去重表。落库前 WriteEventBatch 先 INSERT ... ON CONFLICT DO NOTHING
-- 把本批次 EventUID 登记于此，已存在即视为 Stream 重投、跳过整条事件（防重复落库/扣费）。
-- 与 ad_events 解耦：不碰分区表 ad_events 的唯一索引（分区表加全局唯一索引需含分区键），
-- 单独小表做幂等闸门更干净。created_at 供定时清理（事件 ACK 后不再重投，旧记录无价值）。
CREATE TABLE event_dedup (
    event_uid  text        PRIMARY KEY,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_event_dedup_created ON event_dedup (created_at);
