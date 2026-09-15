-- 「系统设置」改为纯分组：父级菜单点击只展开子菜单，不再跳转到
-- /settings（该路由会 redirect 到 /settings/pricing，误导入「其他配置」页面）。
-- 子菜单仍为用户 / 角色 / 菜单管理（system.*）。

UPDATE ads_center.menus
SET href        = NULL,
    parent_code = NULL
WHERE code = 'settings';
