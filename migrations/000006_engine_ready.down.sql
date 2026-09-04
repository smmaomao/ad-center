-- 回滚 000006

ALTER TABLE ads_center.metrics_minute DROP CONSTRAINT metrics_minute_pkey;
ALTER TABLE ads_center.metrics_minute
    ALTER COLUMN advertiser_id DROP DEFAULT;
ALTER TABLE ads_center.metrics_minute
    ADD CONSTRAINT metrics_minute_pkey PRIMARY KEY (slot_id, advertiser_id, minute_ts);

ALTER TABLE ads_center.creatives
    DROP COLUMN IF EXISTS width,
    DROP COLUMN IF EXISTS height,
    DROP COLUMN IF EXISTS duration_ms;

DROP INDEX IF EXISTS ads_center.uq_ad_slots_key;
ALTER TABLE ads_center.ad_slots DROP COLUMN IF EXISTS slot_key;

DROP TRIGGER IF EXISTS trg_creatives_notify ON ads_center.creatives;
DROP TRIGGER IF EXISTS trg_fill_priorities_notify ON ads_center.fill_priorities;
DROP TRIGGER IF EXISTS trg_ad_slots_notify ON ads_center.ad_slots;
DROP TRIGGER IF EXISTS trg_advertisers_notify ON ads_center.advertisers;
DROP TRIGGER IF EXISTS trg_apps_notify ON ads_center.apps;
DROP FUNCTION IF EXISTS ads_center.notify_config_changed();
