-- 产品层（advertiser → product → campaign）：
--   一个广告主公司旗下多款产品各自跑 campaign，按产品维度归集预算 / KPI / 报表。
--   products 挂在广告主下；campaigns 增加 product_id（可空，单产品广告主可留空直挂广告主）。

CREATE TABLE IF NOT EXISTS ads_center.products (
    product_id    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    advertiser_id UUID NOT NULL REFERENCES ads_center.advertisers(advertiser_id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    status        TEXT NOT NULL DEFAULT 'active',
    daily_budget  float8 NOT NULL DEFAULT 0,
    notes         TEXT,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    deleted_at    timestamptz
);

CREATE INDEX IF NOT EXISTS idx_products_advertiser
    ON ads_center.products (advertiser_id) WHERE deleted_at IS NULL;

ALTER TABLE ads_center.campaigns
    ADD COLUMN IF NOT EXISTS product_id UUID
    REFERENCES ads_center.products(product_id) ON DELETE SET NULL;

-- 导航菜单：挂在「广告管理」分组下（advertisers=10, creatives=20 之间）
INSERT INTO ads_center.menus (code, label, href, parent_code, sort_order)
VALUES ('products', '产品管理', '/products', 'ad_mgmt', 15)
ON CONFLICT (code) DO UPDATE SET
    label = EXCLUDED.label, href = EXCLUDED.href,
    parent_code = EXCLUDED.parent_code, sort_order = EXCLUDED.sort_order, enabled = TRUE;

-- 授权：超管 / 运营 / 产品策略 可见
INSERT INTO ads_center.role_menus (role_code, menu_code)
SELECT r.code, 'products'
FROM ads_center.roles r
WHERE r.code IN ('super_admin', 'operator', 'strategy')
ON CONFLICT DO NOTHING;
