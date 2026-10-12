-- 回滚：菜单名改回「实时监控」。
UPDATE ads_center.menus
SET label = '实时监控'
WHERE code = 'dashboard';
