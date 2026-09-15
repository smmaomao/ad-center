-- 应用软删：与 advertisers/slots/creatives/campaigns/products 一致，
-- 删除仅置 deleted_at，保留历史归因与事件数据（ad_events / decision_logs 等外键引用保留）。
ALTER TABLE ads_center.apps ADD COLUMN deleted_at TIMESTAMPTZ;
COMMENT ON COLUMN ads_center.apps.deleted_at IS '软删时间，NULL=未删除';
