-- 新增「其他配置」分组，把三个运行时配置页挂到其下：
--   平台计费标准线 / 决策结果缓存 / 全局频控配置
-- 原「系统设置」保留给 用户管理 / 角色管理 / 菜单管理。
-- （对应 web/app/(dashboard)/settings/{pricing,cache,fatigue} 页面）

INSERT INTO ads_center.menus (code, label, href, parent_code, sort_order)
VALUES ('other_config', '其他配置', NULL, NULL, 45)
ON CONFLICT (code) DO UPDATE SET
    label       = '其他配置',
    href        = NULL,
    parent_code = NULL,
    sort_order  = 45,
    enabled     = TRUE;

INSERT INTO ads_center.menus (code, label, href, parent_code, sort_order)
VALUES
  ('pricing_benchmark', '平台计费标准线', '/settings/pricing', 'other_config', 10),
  ('decision_cache', '决策结果缓存', '/settings/cache', 'other_config', 20),
  ('fatigue', '全局频控配置', '/settings/fatigue', 'other_config', 30)
ON CONFLICT (code) DO UPDATE SET
    label        = EXCLUDED.label,
    href         = EXCLUDED.href,
    parent_code  = EXCLUDED.parent_code,
    sort_order   = EXCLUDED.sort_order,
    enabled      = TRUE;

-- 授权：超管与运营可见
INSERT INTO ads_center.role_menus (role_code, menu_code)
SELECT r.code, m.code
FROM ads_center.roles r
         CROSS JOIN ads_center.menus m
WHERE r.code IN ('super_admin', 'operator')
  AND m.code IN ('pricing_benchmark', 'decision_cache', 'fatigue')
ON CONFLICT DO NOTHING;
