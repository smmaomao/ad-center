-- 被禁用（status=disabled）的账号不得登录后台。
--
-- current_admin_role() 是前端 getSession() 的角色来源（web/lib/auth.ts），
-- 这里加上 status='active' 过滤后，被禁用用户拿不到角色 → 直接被踢回登录页，
-- 不需要前端各处再单独判断状态。
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
      AND status = 'active';
$$;

REVOKE ALL ON FUNCTION public.current_admin_role() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.current_admin_role() TO authenticated, anon;
