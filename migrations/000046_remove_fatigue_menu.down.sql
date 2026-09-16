-- 回滚：恢复「全局频控配置」菜单（超管与运营可见）。
INSERT INTO ads_center.menus (code, label, href, parent_code, sort_order)
VALUES ('fatigue', '全局频控配置', '/settings/fatigue', 'other_config', 30)
ON CONFLICT (code) DO UPDATE SET
    label        = EXCLUDED.label,
    href         = EXCLUDED.href,
    parent_code  = EXCLUDED.parent_code,
    sort_order   = EXCLUDED.sort_order,
    enabled      = TRUE;

INSERT INTO ads_center.role_menus (role_code, menu_code)
SELECT r.code, 'fatigue'
FROM ads_center.roles r
WHERE r.code IN ('super_admin', 'operator')
ON CONFLICT DO NOTHING;
