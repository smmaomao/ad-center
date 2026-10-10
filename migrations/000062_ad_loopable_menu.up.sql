-- 新增「广告列表循环开关」菜单（对应 web/app/(dashboard)/settings/loop 页面）
-- 该开关是 settings 表里的 ad_loopable（总控制，开启后 data.loopable 强制为 true）。

INSERT INTO ads_center.menus (code, label, href, parent_code, sort_order)
VALUES ('ad_loopable', '广告列表循环开关', '/settings/loop', 'other_config', 30)
ON CONFLICT (code) DO UPDATE SET
    label       = EXCLUDED.label,
    href        = EXCLUDED.href,
    parent_code = EXCLUDED.parent_code,
    sort_order  = EXCLUDED.sort_order,
    enabled     = TRUE;

-- 授权：超管与运营可见
INSERT INTO ads_center.role_menus (role_code, menu_code)
SELECT r.code, 'ad_loopable'
FROM ads_center.roles r
WHERE r.code IN ('super_admin', 'operator')
ON CONFLICT DO NOTHING;
