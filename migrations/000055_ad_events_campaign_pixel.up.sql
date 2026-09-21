-- V1.4：S2S 转化回调落库时补充"任务标识"，便于按广告任务查看与统计/对账。
--
-- campaign_id：服务端从 click 反查得到的权威任务归属（creative → campaign）。
-- pixel_id：归因方/中介（berealads）回传的 pixel/任务标识，便于对照中介侧标记。
-- 两列均为可空 TEXT，向后兼容历史事件行（历史行为空）。
-- ad_events 为分区表，ALTER/CREATE INDEX 会自动级联到各分区。

ALTER TABLE ads_center.ad_events
    ADD COLUMN IF NOT EXISTS campaign_id TEXT;
ALTER TABLE ads_center.ad_events
    ADD COLUMN IF NOT EXISTS pixel_id TEXT;
CREATE INDEX IF NOT EXISTS idx_ad_events_campaign_ts
    ON ads_center.ad_events (campaign_id, ts);
