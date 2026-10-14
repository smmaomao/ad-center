DROP INDEX IF EXISTS ads_center.idx_clicks_campaign_id;
ALTER TABLE ads_center.clicks DROP COLUMN IF EXISTS campaign_id;
