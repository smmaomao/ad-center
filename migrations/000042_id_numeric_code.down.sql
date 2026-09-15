-- 000042 down（落法 B）：撤销 id 数字化 + code + *_code 改名
--
-- 落法 B 下 app_code/slot_code/role_code 等本就存业务字符串、引用 code 列，
-- 故回退时无需「数字 id -> 业务键」回填，直接改回列名 + 还原维度表 id 即可。

-- 1) 删除 000042 重建的 11 条外键（分区表父表级，自动级联子表）
ALTER TABLE ad_events DROP CONSTRAINT IF EXISTS ad_events_app_id_fkey;
ALTER TABLE ad_slots DROP CONSTRAINT IF EXISTS ad_slots_app_id_fkey;
ALTER TABLE admin_users DROP CONSTRAINT IF EXISTS admin_users_role_fk;
ALTER TABLE budget_ledger DROP CONSTRAINT IF EXISTS budget_ledger_app_id_fkey;
ALTER TABLE decision_logs DROP CONSTRAINT IF EXISTS decision_logs_slot_id_fkey;
ALTER TABLE decision_logs DROP CONSTRAINT IF EXISTS decision_logs_app_id_fkey;
ALTER TABLE fill_priorities DROP CONSTRAINT IF EXISTS fill_priorities_slot_id_fkey;
ALTER TABLE menus DROP CONSTRAINT IF EXISTS menus_parent_code_fkey;
ALTER TABLE metrics_minute DROP CONSTRAINT IF EXISTS metrics_minute_app_id_fkey;
ALTER TABLE role_menus DROP CONSTRAINT IF EXISTS role_menus_role_code_fkey;
ALTER TABLE role_menus DROP CONSTRAINT IF EXISTS role_menus_menu_code_fkey;

-- 2) 关联列改回原名称（值保持业务字符串，无需重映射）
ALTER TABLE ad_events RENAME COLUMN app_code TO app_id;
ALTER TABLE ad_slots RENAME COLUMN app_code TO app_id;
ALTER TABLE budget_ledger RENAME COLUMN app_code TO app_id;
ALTER TABLE decision_logs RENAME COLUMN app_code TO app_id;
ALTER TABLE metrics_minute RENAME COLUMN app_code TO app_id;
ALTER TABLE clicks RENAME COLUMN app_code TO app_id;
ALTER TABLE decision_logs ALTER COLUMN slot_code TYPE uuid USING slot_code::uuid;
ALTER TABLE decision_logs RENAME COLUMN slot_code TO slot_id;
ALTER TABLE fill_priorities ALTER COLUMN slot_code TYPE uuid USING slot_code::uuid;
ALTER TABLE fill_priorities RENAME COLUMN slot_code TO slot_id;
ALTER TABLE admin_users RENAME COLUMN role_code TO role;

-- 3) 维度表 id 还原为原始类型（uuid/text），code 即原值
ALTER TABLE apps DROP CONSTRAINT apps_pkey;
ALTER TABLE apps DROP COLUMN id;
ALTER TABLE apps ADD COLUMN id text;
UPDATE apps SET id = code;
ALTER TABLE apps ADD PRIMARY KEY (id);
ALTER TABLE apps DROP COLUMN code;

ALTER TABLE ad_slots DROP CONSTRAINT ad_slots_pkey;
ALTER TABLE ad_slots DROP COLUMN id;
ALTER TABLE ad_slots ADD COLUMN id uuid;
UPDATE ad_slots SET id = code::uuid;
ALTER TABLE ad_slots ADD PRIMARY KEY (id);
ALTER TABLE ad_slots DROP COLUMN code;

ALTER TABLE admin_users DROP CONSTRAINT admin_users_pkey;
ALTER TABLE admin_users DROP COLUMN id;
ALTER TABLE admin_users ADD COLUMN id uuid;
UPDATE admin_users SET id = code::uuid;
ALTER TABLE admin_users ADD PRIMARY KEY (id);
ALTER TABLE admin_users DROP COLUMN code;

ALTER TABLE fill_priorities DROP CONSTRAINT fill_priorities_pkey;
ALTER TABLE fill_priorities DROP COLUMN id;
ALTER TABLE fill_priorities ADD COLUMN id uuid;
UPDATE fill_priorities SET id = code::uuid;
ALTER TABLE fill_priorities ADD PRIMARY KEY (id);
ALTER TABLE fill_priorities DROP COLUMN code;

ALTER TABLE roles DROP CONSTRAINT roles_pkey;
ALTER TABLE roles DROP COLUMN id;
ALTER TABLE roles ADD COLUMN id text;
UPDATE roles SET id = code;
ALTER TABLE roles ADD PRIMARY KEY (id);
ALTER TABLE roles DROP COLUMN code;

ALTER TABLE menus DROP CONSTRAINT menus_pkey;
ALTER TABLE menus DROP COLUMN id;
ALTER TABLE menus ADD COLUMN id text;
UPDATE menus SET id = code;
ALTER TABLE menus ADD PRIMARY KEY (id);
ALTER TABLE menus DROP COLUMN code;

ALTER TABLE settings DROP CONSTRAINT settings_pkey;
ALTER TABLE settings DROP COLUMN id;
ALTER TABLE settings ADD COLUMN id text;
UPDATE settings SET id = code;
ALTER TABLE settings ADD PRIMARY KEY (id);
ALTER TABLE settings DROP COLUMN code;

-- 4) 重建原始外键（含分区子表）
ALTER TABLE ad_events_2026_09 DROP CONSTRAINT IF EXISTS ad_events_app_id_fkey;
ALTER TABLE ad_events_2026_10 DROP CONSTRAINT IF EXISTS ad_events_app_id_fkey;
ALTER TABLE ad_events_2026_11 DROP CONSTRAINT IF EXISTS ad_events_app_id_fkey;
ALTER TABLE ad_events_2026_12 DROP CONSTRAINT IF EXISTS ad_events_app_id_fkey;
ALTER TABLE ad_events_2027_01 DROP CONSTRAINT IF EXISTS ad_events_app_id_fkey;
ALTER TABLE ad_events_2027_02 DROP CONSTRAINT IF EXISTS ad_events_app_id_fkey;
ALTER TABLE decision_logs_2026_09 DROP CONSTRAINT IF EXISTS decision_logs_app_id_fkey;
ALTER TABLE decision_logs_2026_09 DROP CONSTRAINT IF EXISTS decision_logs_slot_id_fkey;
ALTER TABLE decision_logs_2026_10 DROP CONSTRAINT IF EXISTS decision_logs_app_id_fkey;
ALTER TABLE decision_logs_2026_10 DROP CONSTRAINT IF EXISTS decision_logs_slot_id_fkey;
ALTER TABLE decision_logs_2026_11 DROP CONSTRAINT IF EXISTS decision_logs_app_id_fkey;
ALTER TABLE decision_logs_2026_11 DROP CONSTRAINT IF EXISTS decision_logs_slot_id_fkey;
ALTER TABLE decision_logs_2026_12 DROP CONSTRAINT IF EXISTS decision_logs_app_id_fkey;
ALTER TABLE decision_logs_2026_12 DROP CONSTRAINT IF EXISTS decision_logs_slot_id_fkey;
ALTER TABLE decision_logs_2027_01 DROP CONSTRAINT IF EXISTS decision_logs_app_id_fkey;
ALTER TABLE decision_logs_2027_01 DROP CONSTRAINT IF EXISTS decision_logs_slot_id_fkey;
ALTER TABLE decision_logs_2027_02 DROP CONSTRAINT IF EXISTS decision_logs_app_id_fkey;
ALTER TABLE decision_logs_2027_02 DROP CONSTRAINT IF EXISTS decision_logs_slot_id_fkey;

ALTER TABLE ad_slots ADD CONSTRAINT ad_slots_app_id_fkey FOREIGN KEY (app_id) REFERENCES ads_center.apps(id);
ALTER TABLE admin_users ADD CONSTRAINT admin_users_role_fk FOREIGN KEY (role) REFERENCES ads_center.roles(id);
ALTER TABLE budget_ledger ADD CONSTRAINT budget_ledger_app_id_fkey FOREIGN KEY (app_id) REFERENCES ads_center.apps(id);
ALTER TABLE decision_logs ADD CONSTRAINT decision_logs_slot_id_fkey FOREIGN KEY (slot_id) REFERENCES ads_center.ad_slots(id);
ALTER TABLE decision_logs ADD CONSTRAINT decision_logs_app_id_fkey FOREIGN KEY (app_id) REFERENCES ads_center.apps(id);
ALTER TABLE fill_priorities ADD CONSTRAINT fill_priorities_slot_id_fkey FOREIGN KEY (slot_id) REFERENCES ads_center.ad_slots(id);
ALTER TABLE menus ADD CONSTRAINT menus_parent_code_fkey FOREIGN KEY (parent_code) REFERENCES ads_center.menus(id);
ALTER TABLE metrics_minute ADD CONSTRAINT metrics_minute_app_id_fkey FOREIGN KEY (app_id) REFERENCES ads_center.apps(id);
ALTER TABLE role_menus ADD CONSTRAINT role_menus_role_code_fkey FOREIGN KEY (role_code) REFERENCES ads_center.roles(id);
ALTER TABLE role_menus ADD CONSTRAINT role_menus_menu_code_fkey FOREIGN KEY (menu_code) REFERENCES ads_center.menus(id);
ALTER TABLE ad_events ADD CONSTRAINT ad_events_app_id_fkey FOREIGN KEY (app_id) REFERENCES ads_center.apps(id);

-- 5) RPC 还原为按 id 匹配（auth.uid() 直接等于 admin_users.id）
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
