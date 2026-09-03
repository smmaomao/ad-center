-- 后台角色查询函数：暴露在 public（PostgREST 默认只暴露 public schema），
-- SECURITY DEFINER 读取 ads_center.admin_users，供 Next.js RPC 调用做 RBAC。
CREATE OR REPLACE FUNCTION public.current_admin_role()
RETURNS TEXT
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = ads_center, public
AS $$
    SELECT role
    FROM ads_center.admin_users
    WHERE auth_user_id = auth.uid()
$$;

REVOKE ALL ON FUNCTION public.current_admin_role() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.current_admin_role() TO authenticated, anon;
