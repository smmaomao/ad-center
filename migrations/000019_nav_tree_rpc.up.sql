-- current_admin_menus 改为返回"被授权菜单 + 它们的父链"
--
-- 之前只返被授权菜单：若某角色只勾了子菜单(system.users)而父分组(settings)未授权，
-- 子菜单会变成孤儿根，侧边栏分组框架就丢了。这里用 WITH RECURSIVE 向上找父链，
-- 保证前端能正确拼出树。
CREATE OR REPLACE FUNCTION public.current_admin_menus()
RETURNS TABLE (menu_code text, menu_label text, menu_href text,
               menu_parent_code text, menu_sort_order int)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = ads_center, public
AS $$
    WITH RECURSIVE granted AS (
        SELECT rm.menu_code
        FROM ads_center.role_menus rm
                 JOIN ads_center.admin_users u ON u.role = rm.role_code
        WHERE u.auth_user_id = auth.uid()
          AND u.status = 'active'
    ), tree AS (
        SELECT m.code, m.label, m.href, m.parent_code, m.sort_order
        FROM ads_center.menus m
        WHERE m.enabled
          AND m.code IN (SELECT menu_code FROM granted)
        UNION
        SELECT m.code, m.label, m.href, m.parent_code, m.sort_order
        FROM ads_center.menus m
                 JOIN tree t ON t.parent_code = m.code
        WHERE m.enabled
    )
    SELECT DISTINCT code, label, href, parent_code, sort_order
    FROM tree
    ORDER BY parent_code NULLS FIRST, sort_order, code;
$$;

REVOKE ALL ON FUNCTION public.current_admin_menus() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.current_admin_menus() TO authenticated, anon;