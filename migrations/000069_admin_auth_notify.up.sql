-- RBAC 进程内缓存的跨实例失效广播（写端）
--
-- 后台的 GetAdminRole / GetAdminMenu 结果在进程内短缓存（TTL 5min，见
-- internal/store/admin_auth.go）。授权表变更后必须让**所有**实例失效，
-- 否则多实例部署时未处理写请求的实例会继续用旧缓存放行（降权 / 删号后
-- 残留旧权限）。这里用触发器广播，把失效窗口从 TTL 缩到毫秒级。
--
-- 通道: ads_center_admin_auth_changed，payload: 表名
-- 监听端: (*store.Store).RunAdminAuthListener（internal/store/listener.go）

CREATE OR REPLACE FUNCTION ads_center.notify_admin_auth_changed()
RETURNS TRIGGER AS $$
BEGIN
    PERFORM pg_notify('ads_center_admin_auth_changed', TG_TABLE_NAME);
    RETURN NEW;
END $$ LANGUAGE plpgsql;

CREATE TRIGGER trg_admin_users_notify
    AFTER INSERT OR UPDATE OR DELETE ON ads_center.admin_users
    FOR EACH ROW EXECUTE FUNCTION ads_center.notify_admin_auth_changed();

CREATE TRIGGER trg_roles_notify
    AFTER INSERT OR UPDATE OR DELETE ON ads_center.roles
    FOR EACH ROW EXECUTE FUNCTION ads_center.notify_admin_auth_changed();

CREATE TRIGGER trg_menus_notify
    AFTER INSERT OR UPDATE OR DELETE ON ads_center.menus
    FOR EACH ROW EXECUTE FUNCTION ads_center.notify_admin_auth_changed();

CREATE TRIGGER trg_role_menus_notify
    AFTER INSERT OR UPDATE OR DELETE ON ads_center.role_menus
    FOR EACH ROW EXECUTE FUNCTION ads_center.notify_admin_auth_changed();
