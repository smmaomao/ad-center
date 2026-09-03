-- 回滚：投放截止时间 + 素材规格
DROP INDEX IF EXISTS ads_center.idx_creatives_orientation;
ALTER TABLE ads_center.creatives
    DROP CONSTRAINT IF EXISTS creatives_media_type_check;
ALTER TABLE ads_center.creatives
    ADD CONSTRAINT creatives_media_type_check CHECK (media_type IN ('video', 'image'));
ALTER TABLE ads_center.creatives DROP COLUMN IF EXISTS orientation;
ALTER TABLE ads_center.advertisers DROP COLUMN IF EXISTS end_at;
