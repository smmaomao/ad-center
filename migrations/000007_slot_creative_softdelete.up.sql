-- V1.3：广告位与素材软删（管理 API CRUD 完成）
-- ad_events 对 slot_id/creative_id 有 FK（无级联），硬删在有事件的行上必然失败；
-- 与 advertisers.deleted_at 保持同一软删模式，快照加载过滤 deleted_at IS NULL。
-- creatives 补 updated_by：与 ad_slots/advertisers 一致的操作者审计列。

ALTER TABLE ads_center.ad_slots ADD COLUMN deleted_at TIMESTAMPTZ;
ALTER TABLE ads_center.creatives ADD COLUMN deleted_at TIMESTAMPTZ;
ALTER TABLE ads_center.creatives ADD COLUMN updated_by TEXT;

COMMENT ON COLUMN ads_center.ad_slots.deleted_at IS '软删时间，NULL=未删除';
COMMENT ON COLUMN ads_center.creatives.deleted_at IS '软删时间，NULL=未删除';
COMMENT ON COLUMN ads_center.creatives.updated_by IS '最后操作者（审计）';
