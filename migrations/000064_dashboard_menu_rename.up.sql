-- 总览：侧边栏菜单「实时监控」改名为「总览」。
-- 该页面已改为手动刷新（无自动轮询、不再实时），名称随之调整。
-- 真实菜单取自 ads_center.menus（000015 以 ON CONFLICT DO NOTHING 播种），
-- 改旧迁移无效，故此处用 UPDATE 改名；语句幂等，可重复执行。
UPDATE ads_center.menus
SET label = '总览'
WHERE code = 'dashboard';
