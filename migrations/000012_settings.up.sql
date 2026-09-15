-- V1.6：运行时设置表（决策缓存开关/时长等后台可配置项）
--
-- settings 为键值表，决策路径在每次请求时从配置快照读取（热更新：
-- 任何写都触发 notify_config_changed，配置缓存秒级重载，无需重启）。
-- 决策缓存默认开启、TTL 5 分钟（300s）。

CREATE TABLE IF NOT EXISTS ads_center.settings (
    key        TEXT PRIMARY KEY,
    value      JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 纳入 ConfigCache 通知：设置变更即时生效（决策缓存时长/开关热更）
CREATE TRIGGER trg_settings_notify
    AFTER INSERT OR UPDATE OR DELETE ON ads_center.settings
    FOR EACH ROW EXECUTE FUNCTION ads_center.notify_config_changed();

-- 决策缓存默认配置
INSERT INTO ads_center.settings (key, value)
VALUES ('decision_cache', '{"enabled":true,"ttl_seconds":300}'::jsonb)
ON CONFLICT (key) DO NOTHING;
