-- V1.3：让 RBAC 真正驱动后台（角色可自定义 + 菜单从库里读）
--
-- 000015 建了 roles / menus / role_menus，但还差两处才能真正生效：
--  1. 000001 把 admin_users.role 写死为 4 个枚举值，自定义角色建出来也分不出去；
--  2. 前端拿不到"当前用户能看到哪些菜单"（ads_center 不直接对 PostgREST 暴露）。

-- 1) 角色改为外键引用 roles(code)：预置的 4 个角色已存在，存量数据天然满足约束
ALTER TABLE ads_center.admin_users
    DROP CONSTRAINT IF EXISTS admin_users_role_check;
ALTER TABLE ads_center.admin_users
    DROP CONSTRAINT IF EXISTS admin_users_role_fk;
ALTER TABLE ads_center.admin_users
    ADD CONSTRAINT admin_users_role_fk FOREIGN KEY (role) REFERENCES ads_center.roles (code);

-- 2) 当前登录管理员可见菜单（前端侧边栏与页面鉴权的数据源）。
--    与 current_admin_role() 同一套路：SECURITY DEFINER + 暴露在 public，
--    因为 PostgREST 默认只暴露 public schema。
--    注意：OUT 参数加了 menu_ 前缀，避免与函数体内 m.code 等列名产生歧义。
CREATE OR REPLACE FUNCTION public.current_admin_menus()
RETURNS TABLE (menu_code text, menu_label text, menu_href text,
               menu_parent_code text, menu_sort_order int)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = ads_center, public
AS $$
    SELECT m.code, m.label, m.href, m.parent_code, m.sort_order
    FROM ads_center.role_menus rm
             JOIN ads_center.menus m ON m.code = rm.menu_code
             JOIN ads_center.admin_users u ON u.role = rm.role_code
    WHERE u.auth_user_id = auth.uid()
      AND u.status = 'active'
      AND m.enabled
    ORDER BY m.sort_order, m.code;
$$;

REVOKE ALL ON FUNCTION public.current_admin_menus() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.current_admin_menus() TO authenticated, anon;
