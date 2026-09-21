-- 回滚 000056：删除 ad_conversions 表（转化回传将重新由代码路径写回 ad_events，
-- 但 000054 的 event_type CHECK 已包含 install/activate 等，故可兼容）。
DROP TABLE IF EXISTS ads_center.ad_conversions;
