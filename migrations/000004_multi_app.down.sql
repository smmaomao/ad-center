-- 回滚多 App 隔离
ALTER TABLE ads_center.budget_ledger DROP COLUMN IF EXISTS app_id;
ALTER TABLE ads_center.decision_logs DROP COLUMN IF EXISTS app_id;
ALTER TABLE ads_center.metrics_minute DROP COLUMN IF EXISTS app_id;
ALTER TABLE ads_center.ad_events DROP COLUMN IF EXISTS app_id;
ALTER TABLE ads_center.ad_slots DROP COLUMN IF EXISTS app_id;
DROP TABLE IF EXISTS ads_center.apps CASCADE;
