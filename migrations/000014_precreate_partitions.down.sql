-- 分区承载数据后不回滚：多出来的空分区无副作用，删掉反而会在写入时报错。
-- 确实需要清理某个月份时手动执行：
--   DROP TABLE IF EXISTS ads_center.ad_events_YYYY_MM;
--   DROP TABLE IF EXISTS ads_center.decision_logs_YYYY_MM;
SELECT 1;
