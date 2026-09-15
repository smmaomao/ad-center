-- 让"系统设置"成为 3 个子菜单的父分组
--
-- 之前 "系统管理"(code=system) 是一个无 href 的占位分组，而用户/角色/菜单三个
-- 子菜单挂在这个分组下。但侧边栏要展示成"系统设置"父 + 3 个子的形式，所以把
-- 子菜单的 parent_code 直接挂到 settings 上、删掉空壳 system。删 system 时外键
-- ON DELETE CASCADE 会连带清掉 role_menus 里对 system 的授权（无害）。
UPDATE ads_center.menus
SET parent_code = 'settings'
WHERE code IN ('system.users', 'system.roles', 'system.menus');

DELETE FROM ads_center.menus WHERE code = 'system';