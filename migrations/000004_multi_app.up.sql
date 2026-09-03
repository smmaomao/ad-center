-- V1.2：多客户端 App 隔离（多租户设计）
-- App 身份由服务端从 API Key 推导（客户端不可自行声明），用户身份 = (app_id, device_id)
-- 广告主全局共享（预算/KPI/素材跨 App 一份），广告位归属具体 App

-- 客户端 App 注册表（API Key 鉴权）
CREATE TABLE ads_center.apps (
    app_id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name           TEXT NOT NULL,
    api_key_prefix TEXT NOT NULL,               -- 展示用前缀（如 adc_appa_9f2e）
    api_key_hash   TEXT NOT NULL UNIQUE,        -- 完整 key 的 sha256 hex，原文不落库
    status         TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'paused')),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TRIGGER trg_apps_touch BEFORE UPDATE ON ads_center.apps
    FOR EACH ROW EXECUTE FUNCTION ads_center.touch_updated_at();

-- 广告位归属 App（A 的开屏 ≠ B 的开屏，填充策略独立配置）
ALTER TABLE ads_center.ad_slots
    ADD COLUMN app_id UUID NOT NULL REFERENCES ads_center.apps(app_id);
CREATE INDEX idx_ad_slots_app ON ads_center.ad_slots (app_id, status);

-- 事件/指标/决策/预算流水补 App 维度（看板筛选与归因）
ALTER TABLE ads_center.ad_events
    ADD COLUMN app_id UUID NOT NULL REFERENCES ads_center.apps(app_id);
CREATE INDEX idx_ad_events_app_ts ON ads_center.ad_events (app_id, ts);

ALTER TABLE ads_center.metrics_minute
    ADD COLUMN app_id UUID NOT NULL REFERENCES ads_center.apps(app_id);
CREATE INDEX idx_metrics_minute_app_ts ON ads_center.metrics_minute (app_id, minute_ts);

-- AI 决策日志：经广告位可推导 App，此列冗余存储便于按 App 检索（P1 才有数据）
ALTER TABLE ads_center.decision_logs
    ADD COLUMN app_id UUID REFERENCES ads_center.apps(app_id);

-- 预算流水：广告主预算全局共享，此列仅做归因（哪次扣减来自哪个 App）
ALTER TABLE ads_center.budget_ledger
    ADD COLUMN app_id UUID REFERENCES ads_center.apps(app_id);
