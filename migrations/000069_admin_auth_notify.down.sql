-- 回滚 000069：移除 RBAC 缓存失效广播的触发器与函数。
-- 注意：回滚后多实例部署的缓存失效退化为纯 TTL（5min）兜底。

DROP TRIGGER IF EXISTS trg_admin_users_notify ON ads_center.admin_users;
DROP TRIGGER IF EXISTS trg_roles_notify ON ads_center.roles;
DROP TRIGGER IF EXISTS trg_menus_notify ON ads_center.menus;
DROP TRIGGER IF EXISTS trg_role_menus_notify ON ads_center.role_menus;
DROP FUNCTION IF EXISTS ads_center.notify_admin_auth_changed();
