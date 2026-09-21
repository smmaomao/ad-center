DROP INDEX IF EXISTS idx_ad_events_campaign_ts;
ALTER TABLE ads_center.ad_events
    DROP COLUMN IF EXISTS campaign_id;
ALTER TABLE ads_center.ad_events
    DROP COLUMN IF EXISTS pixel_id;
