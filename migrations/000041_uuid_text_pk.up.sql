-- 000041_uuid_text_pk: uuid/text 业务键表主键统一命名为 id（保持原类型，仅改名）
--
-- 仅改各表「主键列」的列名：apps.app_id→id、ad_slots.slot_id→id、
-- admin_users.auth_user_id→id、roles.code→id、menus.code→id、settings.key→id。
-- 例外：clicks 保留对外业务键 click_id（TEXT，归因链接 / S2S 回传引用），
-- 另增 id 自增主键（与 decision_logs 同样「业务键 + 代理 id」模式），见下方单独处理。
-- 其余表的「外键列」保持不变：app_id / slot_id / auth_user_id / click_id /
-- role_code / menu_code / admin_users.role / menus.parent_code。
-- RENAME COLUMN 自动更新所有外键约束与引用这些列的 RPC 函数签名之外的
-- 函数体（函数体需在本迁移里手动重建，见下文）。

ALTER TABLE apps        RENAME COLUMN app_id        TO id;
ALTER TABLE ad_slots    RENAME COLUMN slot_id       TO id;
ALTER TABLE admin_users RENAME COLUMN auth_user_id  TO id;
-- clicks：保留对外业务键 click_id（TEXT，归因链接 / S2S 回传引用），
-- 新增 id 自增主键（与 decision_logs 同样「业务键 + 代理 id」模式）。
ALTER TABLE clicks DROP CONSTRAINT IF EXISTS clicks_pkey;
ALTER TABLE clicks ADD COLUMN id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY;
ALTER TABLE clicks ADD CONSTRAINT clicks_click_id_key UNIQUE (click_id);
ALTER TABLE roles       RENAME COLUMN code          TO id;
ALTER TABLE menus       RENAME COLUMN code          TO id;
ALTER TABLE settings    RENAME COLUMN key           TO id;

-- RPC: current_admin_menus 引用了 roles.id / menus.id / admin_users.id，
-- 必须同步重建函数体（SECURITY DEFINER 等属性保持不变）。
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
        WHERE u.id = auth.uid()
          AND u.status = 'active'
    ), tree AS (
        SELECT m.id, m.label, m.href, m.parent_code, m.sort_order
        FROM ads_center.menus m
        WHERE m.enabled
          AND m.id IN (SELECT menu_code FROM granted)
        UNION
        SELECT m.id, m.label, m.href, m.parent_code, m.sort_order
        FROM ads_center.menus m
                 JOIN tree t ON t.parent_code = m.id
        WHERE m.enabled
    )
    SELECT DISTINCT id, label, href, parent_code, sort_order
    FROM tree
    ORDER BY parent_code NULLS FIRST, sort_order, id;
$$;

REVOKE ALL ON FUNCTION public.current_admin_menus() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.current_admin_menus() TO authenticated, anon;

-- RPC: current_admin_role 引用了 admin_users.id
CREATE OR REPLACE FUNCTION public.current_admin_role()
RETURNS TEXT
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = ads_center, public
AS $$
    SELECT role
    FROM ads_center.admin_users
    WHERE id = auth.uid()
      AND status = 'active';
$$;

REVOKE ALL ON FUNCTION public.current_admin_role() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.current_admin_role() TO authenticated, anon;
