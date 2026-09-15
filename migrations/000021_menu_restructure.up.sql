-- 菜单重构（去 slot 改造的一部分）：
--  1. 新增「广告管理」父菜单（无 href，纯分组）
--  2. 广告主管理、广告素材管理 挂到该父菜单下
--  3. 广告位管理停用（表与数据保留，以后想恢复改 enabled 即可）
--  4. 新增「应用管理」

INSERT INTO ads_center.menus (code, label, href, sort_order)
VALUES ('ad_mgmt', '广告管理', NULL, 15)
ON CONFLICT (code) DO UPDATE SET label = EXCLUDED.label, sort_order = EXCLUDED.sort_order;

UPDATE ads_center.menus SET parent_code = 'ad_mgmt', sort_order = 10
WHERE code = 'advertisers';

INSERT INTO ads_center.menus (code, label, href, parent_code, sort_order)
VALUES ('creatives', '广告素材管理', '/creatives', 'ad_mgmt', 20)
ON CONFLICT (code) DO UPDATE SET label = EXCLUDED.label, href = EXCLUDED.href,
    parent_code = EXCLUDED.parent_code, sort_order = EXCLUDED.sort_order, enabled = TRUE;

UPDATE ads_center.menus SET enabled = FALSE WHERE code = 'slots';

INSERT INTO ads_center.menus (code, label, href, sort_order)
VALUES ('apps', '应用管理', '/apps', 5)
ON CONFLICT (code) DO UPDATE SET label = EXCLUDED.label, href = EXCLUDED.href, enabled = TRUE;

-- 授权：超管与运营可见新增菜单
INSERT INTO ads_center.role_menus (role_code, menu_code)
SELECT r.code, m.code
FROM ads_center.roles r
         CROSS JOIN ads_center.menus m
WHERE r.code IN ('super_admin', 'operator')
  AND m.code IN ('ad_mgmt', 'creatives', 'apps')
ON CONFLICT DO NOTHING;
