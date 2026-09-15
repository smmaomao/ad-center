-- 广告任务（Campaign）+ 素材公共库化
-- 「广告管理」改为 主从两层 + 独立素材库：
--   广告主(预算/余额闸, 防资损) → 广告任务(出价/KPI/单价/频控 + 关联素材) → 素材库(公共)

CREATE TABLE ads_center.campaigns (
    campaign_id      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    advertiser_id    UUID NOT NULL REFERENCES ads_center.advertisers(advertiser_id) ON DELETE CASCADE,
    name             TEXT NOT NULL,
    status           TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'paused')),

    -- 出价 / KPI / 单价（口径与广告主对齐）
    bidding_mode     TEXT NOT NULL DEFAULT 'cpi' CHECK (bidding_mode IN ('cpi', 'cpa', 'revenue_share')),
    bidding_price    NUMERIC(12, 6) NOT NULL DEFAULT 0,
    bidding_price_min NUMERIC(12, 6) NOT NULL DEFAULT 0,

    -- 计费方式（cpm/cpc/cpa），cpa 时按 cpa_event_prices 区间扣费
    billing_mode     TEXT NOT NULL DEFAULT 'cpm' CHECK (billing_mode IN ('cpm', 'cpc', 'cpa')),
    cpa_event_prices JSONB NOT NULL DEFAULT '{}'::jsonb,
    target_cpi       NUMERIC(12, 6) NOT NULL DEFAULT 0,   -- KPI 目标

    -- 任务级日预算（0 = 跟随广告主）
    daily_budget     NUMERIC(12, 2) NOT NULL DEFAULT 0,

    -- 频控（任务级，覆盖广告位默认）
    freq_daily_limit      SMALLINT NOT NULL DEFAULT 8,
    freq_interval_minutes SMALLINT NOT NULL DEFAULT 20,
    freq_fatigue_window   SMALLINT NOT NULL DEFAULT 3,

    -- 关联素材（公共创意库 id 列表）
    creative_ids     TEXT[] NOT NULL DEFAULT '{}',

    start_at         TIMESTAMPTZ,
    end_at           TIMESTAMPTZ,
    created_by       TEXT,
    updated_by       TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at       TIMESTAMPTZ
);

CREATE INDEX idx_campaigns_adv ON ads_center.campaigns (advertiser_id, status) WHERE deleted_at IS NULL;

-- 素材公共库化：advertiser_id 可空（不再强制绑定某个广告主）
ALTER TABLE ads_center.creatives ALTER COLUMN advertiser_id DROP NOT NULL;

-- 菜单：广告管理 下新增「广告任务管理」
INSERT INTO ads_center.menus (code, label, href, parent_code, sort_order)
VALUES ('campaigns', '广告任务管理', '/campaigns', 'ad_mgmt', 20)
ON CONFLICT (code) DO UPDATE SET
    label       = '广告任务管理',
    href        = '/campaigns',
    parent_code = 'ad_mgmt',
    sort_order  = 20,
    enabled     = TRUE;

-- 素材管理 改名（去掉「广告」前缀，体现公共库定位），排到任务之后
UPDATE ads_center.menus SET label = '素材管理', sort_order = 30 WHERE code = 'creatives';

-- 授权：超管 + 运营可见
INSERT INTO ads_center.role_menus (role_code, menu_code)
SELECT r.code, 'campaigns'
FROM ads_center.roles r
WHERE r.code IN ('super_admin', 'operator')
ON CONFLICT DO NOTHING;
