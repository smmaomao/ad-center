-- 000041_uuid_text_pk down：回滚 uuid/text 业务键表改名 + 还原 RPC 函数体

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

ALTER TABLE settings    RENAME COLUMN id TO key;
ALTER TABLE menus       RENAME COLUMN id TO code;
ALTER TABLE roles       RENAME COLUMN id TO code;
-- clicks：撤销 id 代理主键，恢复 click_id 为主键。
ALTER TABLE clicks DROP CONSTRAINT IF EXISTS clicks_pkey;
ALTER TABLE clicks DROP CONSTRAINT IF EXISTS clicks_click_id_key;
ALTER TABLE clicks DROP COLUMN IF EXISTS id;
ALTER TABLE clicks ADD CONSTRAINT clicks_pkey PRIMARY KEY (click_id);
ALTER TABLE admin_users RENAME COLUMN id TO auth_user_id;
ALTER TABLE ad_slots    RENAME COLUMN id TO slot_id;
ALTER TABLE apps        RENAME COLUMN id TO app_id;
