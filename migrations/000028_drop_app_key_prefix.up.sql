-- 移除 apps.api_key_prefix：该列仅为后台展示用（Key 前缀），不参与鉴权，
-- 且容易让使用者困惑，故删除。app_id 短字符串（migration 000027）与鉴权逻辑不受影响。
ALTER TABLE ads_center.apps DROP COLUMN IF EXISTS api_key_prefix;
