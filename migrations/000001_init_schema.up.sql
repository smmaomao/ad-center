-- 广告中心初始 schema：ads_center（与其他项目 schema 隔离）
-- 依据 docs/ARCHITECTURE.md 第三章数据库设计

CREATE SCHEMA IF NOT EXISTS ads_center;

-- ============================================================
-- 广告主
-- ============================================================
CREATE TABLE ads_center.advertisers (
    advertiser_id   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            TEXT NOT NULL,
    tier            SMALLINT NOT NULL CHECK (tier IN (1, 2, 3)),
    status          TEXT NOT NULL DEFAULT 'active'
                    CHECK (status IN ('active', 'paused', 'budget_exhausted')),

    -- KPI 与预算（PRD 6.1）
    target_cpi      NUMERIC(12, 6) NOT NULL,          -- 目标出价（KPI），美元
    actual_cpi      NUMERIC(12, 6) NOT NULL DEFAULT 0, -- 实际达成，事件聚合回写
    daily_budget    NUMERIC(12, 2) NOT NULL,           -- 日预算，美元
    spent_today     NUMERIC(12, 2) NOT NULL DEFAULT 0, -- 今日已消耗，进程内+落库对账
    consume_speed   TEXT NOT NULL DEFAULT 'even'
                    CHECK (consume_speed IN ('even', 'accelerated', 'asap')),
    bidding_mode    TEXT NOT NULL DEFAULT 'cpi'
                    CHECK (bidding_mode IN ('cpi', 'cpa', 'revenue_share')),
    bidding_price   NUMERIC(12, 6) NOT NULL DEFAULT 0,

    -- 保底
    guaranteed_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    guaranteed_min_share NUMERIC(5, 4) NOT NULL DEFAULT 0 CHECK (guaranteed_min_share BETWEEN 0 AND 1),

    -- 定向（PRD 6.1 targeting）
    targeting       JSONB NOT NULL DEFAULT '{}'::jsonb,

    -- 实时优先级得分（决策引擎回写，仅供展示）
    priority_score  NUMERIC(12, 6) NOT NULL DEFAULT 0,

    -- 合同与联系人
    contract_start  DATE,
    contract_end    DATE,
    contact         TEXT,

    created_by      TEXT,
    updated_by      TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at      TIMESTAMPTZ                                -- 软删
);

CREATE INDEX idx_advertisers_status_tier ON ads_center.advertisers (status, tier) WHERE deleted_at IS NULL;

-- ============================================================
-- 广告位
-- ============================================================
CREATE TABLE ads_center.ad_slots (
    slot_id     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL,
    type        TEXT NOT NULL CHECK (type IN ('rewarded_video', 'splash', 'interstitial', 'feed')),
    status      TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'paused')),

    -- 频控（PRD FR-04 高级策略，默认值来自 PRD）
    freq_daily_limit       SMALLINT NOT NULL DEFAULT 8,
    freq_interval_minutes  SMALLINT NOT NULL DEFAULT 20,
    freq_fatigue_window    SMALLINT NOT NULL DEFAULT 3,

    -- AI Agent（P1 使用，字段先落）
    ai_agent_enabled       BOOLEAN NOT NULL DEFAULT FALSE,
    ai_agent_goal          TEXT NOT NULL DEFAULT 'kpi_first'
                          CHECK (ai_agent_goal IN ('kpi_first', 'budget_smooth', 'ecpm_max')),

    created_by  TEXT,
    updated_by  TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ============================================================
-- 填充优先级（从 PRD AdSlot 内嵌数组拆独立表，支撑拖拽排序/逐条启停）
-- ============================================================
CREATE TABLE ads_center.fill_priorities (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slot_id         UUID NOT NULL REFERENCES ads_center.ad_slots(slot_id) ON DELETE CASCADE,
    source_type     TEXT NOT NULL CHECK (source_type IN ('advertiser', 'max', 'fallback')),
    advertiser_id   UUID REFERENCES ads_center.advertisers(advertiser_id) ON DELETE CASCADE,
    expected_ecpm   NUMERIC(12, 6) NOT NULL DEFAULT 0,
    guaranteed_share NUMERIC(5, 4) NOT NULL DEFAULT 0 CHECK (guaranteed_share BETWEEN 0 AND 1),
    weight          NUMERIC(8, 4) NOT NULL DEFAULT 1.0,
    enabled         BOOLEAN NOT NULL DEFAULT TRUE,
    position        INT NOT NULL DEFAULT 0,      -- 拖拽排序
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- source_type=advertiser 时必须关联广告主
    CONSTRAINT chk_advertiser_ref CHECK (
        (source_type = 'advertiser' AND advertiser_id IS NOT NULL)
        OR (source_type <> 'advertiser' AND advertiser_id IS NULL)
    )
);

CREATE INDEX idx_fill_priorities_slot ON ads_center.fill_priorities (slot_id, position);
-- 同一广告位内位置唯一（含软停用行，避免排序冲突）
CREATE UNIQUE INDEX uq_fill_priorities_slot_position ON ads_center.fill_priorities (slot_id, position);

-- ============================================================
-- 素材
-- ============================================================
CREATE TABLE ads_center.creatives (
    creative_id     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    advertiser_id   UUID NOT NULL REFERENCES ads_center.advertisers(advertiser_id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    media_type      TEXT NOT NULL CHECK (media_type IN ('video', 'image')),
    storage_path    TEXT NOT NULL,               -- Supabase Storage 对象路径
    file_size_bytes BIGINT NOT NULL DEFAULT 0,
    status          TEXT NOT NULL DEFAULT 'testing'
                    CHECK (status IN ('testing', 'active', 'paused')),
    weight          NUMERIC(8, 4) NOT NULL DEFAULT 1.0,
    ab_group        TEXT,                        -- A/B 分组标识
    -- 素材级指标（事件聚合回写）
    impressions     BIGINT NOT NULL DEFAULT 0,
    clicks          BIGINT NOT NULL DEFAULT 0,
    conversions     BIGINT NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_creatives_advertiser ON ads_center.creatives (advertiser_id, status);

-- ============================================================
-- AI 决策日志（按月分区，保留 180 天）
-- ============================================================
CREATE TABLE ads_center.decision_logs (
    decision_id UUID NOT NULL DEFAULT gen_random_uuid(),
    agent       TEXT NOT NULL CHECK (agent IN ('kpi_guardian', 'budget_controller', 'exposure_optimizer')),
    ts          TIMESTAMPTZ NOT NULL DEFAULT now(),
    action      TEXT NOT NULL,
    target_advertiser_id UUID REFERENCES ads_center.advertisers(advertiser_id),
    slot_id     UUID REFERENCES ads_center.ad_slots(slot_id),
    old_value   NUMERIC(12, 6),
    new_value   NUMERIC(12, 6),
    reason      TEXT NOT NULL,
    kpi_delta   NUMERIC(12, 6),
    fill_rate_delta NUMERIC(12, 6),
    ecpm_delta  NUMERIC(12, 6),
    confidence  NUMERIC(4, 3),
    reverted    BOOLEAN NOT NULL DEFAULT FALSE,
    PRIMARY KEY (decision_id, ts)
) PARTITION BY RANGE (ts);

-- 初始分区：近半年（自动维护任务后续补充，P0 手动建）
DO $$
DECLARE
    m DATE := date_trunc('month', CURRENT_DATE);
BEGIN
    FOR i IN 0..5 LOOP
        EXECUTE format(
            'CREATE TABLE ads_center.decision_logs_%s PARTITION OF ads_center.decision_logs FOR VALUES FROM (%L) TO (%L)',
            to_char(m + (i || ' month')::interval, 'YYYY_MM'),
            (m + (i || ' month')::interval)::date,
            (m + ((i + 1) || ' month')::interval)::date
        );
    END LOOP;
END $$;

CREATE INDEX idx_decision_logs_ts ON ads_center.decision_logs (ts);
CREATE INDEX idx_decision_logs_target ON ads_center.decision_logs (target_advertiser_id, ts);

-- ============================================================
-- 预算流水（对账与平滑控制依据）
-- ============================================================
CREATE TABLE ads_center.budget_ledger (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    advertiser_id   UUID NOT NULL REFERENCES ads_center.advertisers(advertiser_id) ON DELETE CASCADE,
    op_type         TEXT NOT NULL CHECK (op_type IN ('deduct', 'commit', 'rollback', 'calibrate', 'daily_reset')),
    amount          NUMERIC(12, 6) NOT NULL,
    day             DATE NOT NULL DEFAULT CURRENT_DATE,
    hour_bucket     SMALLINT NOT NULL DEFAULT extract(hour FROM now())::smallint,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_budget_ledger_adv_day ON ads_center.budget_ledger (advertiser_id, day);

-- ============================================================
-- 分钟级指标（slot × advertiser 粒度）
-- ============================================================
CREATE TABLE ads_center.metrics_minute (
    slot_id         UUID NOT NULL REFERENCES ads_center.ad_slots(slot_id) ON DELETE CASCADE,
    advertiser_id   UUID,                          -- NULL = 兜底/MAX 聚合
    minute_ts       TIMESTAMPTZ NOT NULL,
    requests        BIGINT NOT NULL DEFAULT 0,
    fills           BIGINT NOT NULL DEFAULT 0,
    impressions     BIGINT NOT NULL DEFAULT 0,
    clicks          BIGINT NOT NULL DEFAULT 0,
    conversions     BIGINT NOT NULL DEFAULT 0,
    revenue         NUMERIC(12, 6) NOT NULL DEFAULT 0,
    PRIMARY KEY (slot_id, advertiser_id, minute_ts)
);

-- ============================================================
-- 原始广告事件（审计回溯，保留 180 天）
-- ============================================================
CREATE TABLE ads_center.ad_events (
    id          BIGINT GENERATED ALWAYS AS IDENTITY,
    event_type  TEXT NOT NULL CHECK (event_type IN ('request', 'fill', 'impression', 'click', 'conversion')),
    ts          TIMESTAMPTZ NOT NULL DEFAULT now(),
    slot_id     UUID NOT NULL REFERENCES ads_center.ad_slots(slot_id),
    advertiser_id UUID REFERENCES ads_center.advertisers(advertiser_id),
    creative_id UUID REFERENCES ads_center.creatives(creative_id),
    device_id   TEXT NOT NULL,
    country     TEXT,
    revenue     NUMERIC(12, 6),
    PRIMARY KEY (id, ts)
) PARTITION BY RANGE (ts);

DO $$
DECLARE
    m DATE := date_trunc('month', CURRENT_DATE);
BEGIN
    FOR i IN 0..5 LOOP
        EXECUTE format(
            'CREATE TABLE ads_center.ad_events_%s PARTITION OF ads_center.ad_events FOR VALUES FROM (%L) TO (%L)',
            to_char(m + (i || ' month')::interval, 'YYYY_MM'),
            (m + (i || ' month')::interval)::date,
            (m + ((i + 1) || ' month')::interval)::date
        );
    END LOOP;
END $$;

CREATE INDEX idx_ad_events_ts ON ads_center.ad_events (ts);
CREATE INDEX idx_ad_events_slot_ts ON ads_center.ad_events (slot_id, ts);
CREATE INDEX idx_ad_events_adv_ts ON ads_center.ad_events (advertiser_id, ts);

-- ============================================================
-- 后台用户角色映射（认证走 Supabase Auth，这里只存角色）
-- ============================================================
CREATE TABLE ads_center.admin_users (
    auth_user_id UUID PRIMARY KEY,          -- 对应 auth.users.id
    email        TEXT NOT NULL,
    role         TEXT NOT NULL DEFAULT 'operator'
                CHECK (role IN ('super_admin', 'operator', 'analyst', 'strategy')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ============================================================
-- 配置变更通知通道（ConfigCache LISTEN/NOTIFY 用）
-- ============================================================
-- NOTIFY 不需要预建通道，这里仅记录约定：
--   通道名: ads_center_config_changed
--   payload: 变更的表名（advertisers / ad_slots / fill_priorities / creatives）

-- ============================================================
-- updated_at 自动维护
-- ============================================================
CREATE OR REPLACE FUNCTION ads_center.touch_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END $$ LANGUAGE plpgsql;

CREATE TRIGGER trg_advertisers_touch BEFORE UPDATE ON ads_center.advertisers
    FOR EACH ROW EXECUTE FUNCTION ads_center.touch_updated_at();
CREATE TRIGGER trg_ad_slots_touch BEFORE UPDATE ON ads_center.ad_slots
    FOR EACH ROW EXECUTE FUNCTION ads_center.touch_updated_at();
CREATE TRIGGER trg_fill_priorities_touch BEFORE UPDATE ON ads_center.fill_priorities
    FOR EACH ROW EXECUTE FUNCTION ads_center.touch_updated_at();
CREATE TRIGGER trg_creatives_touch BEFORE UPDATE ON ads_center.creatives
    FOR EACH ROW EXECUTE FUNCTION ads_center.touch_updated_at();
CREATE TRIGGER trg_admin_users_touch BEFORE UPDATE ON ads_center.admin_users
    FOR EACH ROW EXECUTE FUNCTION ads_center.touch_updated_at();

-- ============================================================
-- 管理操作审计（PRD：可审计，保留 180 天）
-- ============================================================
CREATE TABLE ads_center.audit_logs (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    actor_email TEXT NOT NULL,
    action      TEXT NOT NULL,               -- create / update / delete / pause / ...
    entity_type TEXT NOT NULL,
    entity_id   TEXT NOT NULL,
    diff        JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_audit_logs_entity ON ads_center.audit_logs (entity_type, entity_id, created_at);

-- ============================================================
-- 初始种子：平台兜底广告位来源约定（可按需调整）
-- ============================================================
-- 填充来源中 source_type='max' / 'fallback' 由决策引擎内置，无需种子数据。
