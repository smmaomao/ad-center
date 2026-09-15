-- 回滚：恢复 system 空壳分组
INSERT INTO ads_center.menus (code, label, sort_order)
VALUES ('system', '系统管理', 60)
ON CONFLICT (code) DO NOTHING;

UPDATE ads_center.menus
SET parent_code = 'system'
WHERE code IN ('system.users', 'system.roles', 'system.menus');