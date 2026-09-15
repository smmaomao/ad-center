-- 000042: 维度表 id 转数字 + 原业务键存入 code + 关联列改名 *_code（落法 B）
--
-- 落法 B（用户确认）：
--   app_code / slot_code / role_code 等关联列【直接存业务字符串】（如 4w8uinfere / uuid /
--   admin / dashboard），引用各维度表的 code 列；数据原样不动，不回填数字 id。
--   维度表自身的 id 转成数字（apps=int，其余 bigint）作为内部代理键；原来的业务标识
--   整体搬入 code 列（UNIQUE），对外/鉴权统一用 code。
--   Go 侧基本只改列名：关联列 app_id->app_code、slot_id->slot_code、role->role_code；
--   维度表自身用 code 作为业务标识（SELECT/INSERT 用 code 而非 id）。
--
-- 注意：分区表（ad_events / decision_logs）的外键原始定义在各子表上，
--      本迁移在「父表」上加 FK 即可由 PG12+ 自动级联到所有子表，避免重复。

-- ============================================================
-- 0) 删除所有指向 7 张维度表的外键（含分区子表），便于后续改列/改类型
-- ============================================================
ALTER TABLE ad_events DROP CONSTRAINT IF EXISTS ad_events_app_id_fkey;
ALTER TABLE ad_events_2026_09 DROP CONSTRAINT IF EXISTS ad_events_app_id_fkey;
ALTER TABLE ad_events_2026_10 DROP CONSTRAINT IF EXISTS ad_events_app_id_fkey;
ALTER TABLE ad_events_2026_11 DROP CONSTRAINT IF EXISTS ad_events_app_id_fkey;
ALTER TABLE ad_events_2026_12 DROP CONSTRAINT IF EXISTS ad_events_app_id_fkey;
ALTER TABLE ad_events_2027_01 DROP CONSTRAINT IF EXISTS ad_events_app_id_fkey;
ALTER TABLE ad_events_2027_02 DROP CONSTRAINT IF EXISTS ad_events_app_id_fkey;
ALTER TABLE ad_slots DROP CONSTRAINT IF EXISTS ad_slots_app_id_fkey;
ALTER TABLE admin_users DROP CONSTRAINT IF EXISTS admin_users_role_fk;
ALTER TABLE budget_ledger DROP CONSTRAINT IF EXISTS budget_ledger_app_id_fkey;
ALTER TABLE decision_logs DROP CONSTRAINT IF EXISTS decision_logs_slot_id_fkey;
ALTER TABLE decision_logs DROP CONSTRAINT IF EXISTS decision_logs_app_id_fkey;
ALTER TABLE decision_logs_2026_09 DROP CONSTRAINT IF EXISTS decision_logs_app_id_fkey;
ALTER TABLE decision_logs_2026_09 DROP CONSTRAINT IF EXISTS decision_logs_slot_id_fkey;
ALTER TABLE decision_logs_2026_10 DROP CONSTRAINT IF EXISTS decision_logs_slot_id_fkey;
ALTER TABLE decision_logs_2026_10 DROP CONSTRAINT IF EXISTS decision_logs_app_id_fkey;
ALTER TABLE decision_logs_2026_11 DROP CONSTRAINT IF EXISTS decision_logs_slot_id_fkey;
ALTER TABLE decision_logs_2026_11 DROP CONSTRAINT IF EXISTS decision_logs_app_id_fkey;
ALTER TABLE decision_logs_2026_12 DROP CONSTRAINT IF EXISTS decision_logs_slot_id_fkey;
ALTER TABLE decision_logs_2026_12 DROP CONSTRAINT IF EXISTS decision_logs_app_id_fkey;
ALTER TABLE decision_logs_2027_01 DROP CONSTRAINT IF EXISTS decision_logs_app_id_fkey;
ALTER TABLE decision_logs_2027_01 DROP CONSTRAINT IF EXISTS decision_logs_slot_id_fkey;
ALTER TABLE decision_logs_2027_02 DROP CONSTRAINT IF EXISTS decision_logs_slot_id_fkey;
ALTER TABLE decision_logs_2027_02 DROP CONSTRAINT IF EXISTS decision_logs_app_id_fkey;
ALTER TABLE fill_priorities DROP CONSTRAINT IF EXISTS fill_priorities_slot_id_fkey;
ALTER TABLE menus DROP CONSTRAINT IF EXISTS menus_parent_code_fkey;
ALTER TABLE metrics_minute DROP CONSTRAINT IF EXISTS metrics_minute_app_id_fkey;
ALTER TABLE role_menus DROP CONSTRAINT IF EXISTS role_menus_role_code_fkey;
ALTER TABLE role_menus DROP CONSTRAINT IF EXISTS role_menus_menu_code_fkey;

-- ============================================================
-- 1) 各维度表：加 code 列、拷贝原 id、建唯一约束
-- ============================================================
ALTER TABLE roles ADD COLUMN code text NOT NULL DEFAULT '';
UPDATE roles SET code = id;
ALTER TABLE roles ALTER COLUMN code DROP DEFAULT;
ALTER TABLE roles ADD CONSTRAINT uq_roles_code UNIQUE (code);

ALTER TABLE menus ADD COLUMN code text NOT NULL DEFAULT '';
UPDATE menus SET code = id;
ALTER TABLE menus ALTER COLUMN code DROP DEFAULT;
ALTER TABLE menus ADD CONSTRAINT uq_menus_code UNIQUE (code);

ALTER TABLE apps ADD COLUMN code text NOT NULL DEFAULT '';
UPDATE apps SET code = id;
ALTER TABLE apps ALTER COLUMN code DROP DEFAULT;
ALTER TABLE apps ADD CONSTRAINT uq_apps_code UNIQUE (code);

ALTER TABLE ad_slots ADD COLUMN code text NOT NULL DEFAULT '';
UPDATE ad_slots SET code = id::text;
ALTER TABLE ad_slots ALTER COLUMN code DROP DEFAULT;
ALTER TABLE ad_slots ADD CONSTRAINT uq_ad_slots_code UNIQUE (code);

ALTER TABLE admin_users ADD COLUMN code text NOT NULL DEFAULT '';
UPDATE admin_users SET code = id::text;
ALTER TABLE admin_users ALTER COLUMN code DROP DEFAULT;
ALTER TABLE admin_users ADD CONSTRAINT uq_admin_users_code UNIQUE (code);

ALTER TABLE fill_priorities ADD COLUMN code text NOT NULL DEFAULT '';
UPDATE fill_priorities SET code = id::text;
ALTER TABLE fill_priorities ALTER COLUMN code DROP DEFAULT;
ALTER TABLE fill_priorities ADD CONSTRAINT uq_fill_priorities_code UNIQUE (code);

ALTER TABLE settings ADD COLUMN code text NOT NULL DEFAULT '';
UPDATE settings SET code = id;
ALTER TABLE settings ALTER COLUMN code DROP DEFAULT;
ALTER TABLE settings ADD CONSTRAINT uq_settings_code UNIQUE (code);

-- ============================================================
-- 2) 维度表 id 改为数字（先转被引用方，后转引用方）
-- ============================================================
-- roles -> bigint
ALTER TABLE roles DROP CONSTRAINT roles_pkey;
ALTER TABLE roles DROP COLUMN id;
ALTER TABLE roles ADD COLUMN id bigint GENERATED ALWAYS AS IDENTITY;
ALTER TABLE roles ADD CONSTRAINT roles_pkey PRIMARY KEY (id);

-- menus -> bigint
ALTER TABLE menus DROP CONSTRAINT menus_pkey;
ALTER TABLE menus DROP COLUMN id;
ALTER TABLE menus ADD COLUMN id bigint GENERATED ALWAYS AS IDENTITY;
ALTER TABLE menus ADD CONSTRAINT menus_pkey PRIMARY KEY (id);

-- apps -> int
ALTER TABLE apps DROP CONSTRAINT apps_pkey;
ALTER TABLE apps DROP COLUMN id;
ALTER TABLE apps ADD COLUMN id int GENERATED ALWAYS AS IDENTITY;
ALTER TABLE apps ADD CONSTRAINT apps_pkey PRIMARY KEY (id);

-- ad_slots -> bigint
ALTER TABLE ad_slots DROP CONSTRAINT ad_slots_pkey;
ALTER TABLE ad_slots DROP COLUMN id;
ALTER TABLE ad_slots ADD COLUMN id bigint GENERATED ALWAYS AS IDENTITY;
ALTER TABLE ad_slots ADD CONSTRAINT ad_slots_pkey PRIMARY KEY (id);

-- admin_users -> bigint
ALTER TABLE admin_users DROP CONSTRAINT admin_users_pkey;
ALTER TABLE admin_users DROP COLUMN id;
ALTER TABLE admin_users ADD COLUMN id bigint GENERATED ALWAYS AS IDENTITY;
ALTER TABLE admin_users ADD CONSTRAINT admin_users_pkey PRIMARY KEY (id);

-- fill_priorities -> bigint
ALTER TABLE fill_priorities DROP CONSTRAINT fill_priorities_pkey;
ALTER TABLE fill_priorities DROP COLUMN id;
ALTER TABLE fill_priorities ADD COLUMN id bigint GENERATED ALWAYS AS IDENTITY;
ALTER TABLE fill_priorities ADD CONSTRAINT fill_priorities_pkey PRIMARY KEY (id);

-- settings -> bigint
ALTER TABLE settings DROP CONSTRAINT settings_pkey;
ALTER TABLE settings DROP COLUMN id;
ALTER TABLE settings ADD COLUMN id bigint GENERATED ALWAYS AS IDENTITY;
ALTER TABLE settings ADD CONSTRAINT settings_pkey PRIMARY KEY (id);

-- ============================================================
-- 3) 关联列改名 + 类型调整（落法 B：不回填、不转数字，值保持业务字符串）
-- ============================================================
-- 3a) app_id -> app_code（引用 apps.code，text，值保持业务字符串如 4w8uinfere）
ALTER TABLE ad_events RENAME COLUMN app_id TO app_code;          -- 级联子表
ALTER TABLE ad_slots RENAME COLUMN app_id TO app_code;
ALTER TABLE budget_ledger RENAME COLUMN app_id TO app_code;
ALTER TABLE decision_logs RENAME COLUMN app_id TO app_code;       -- 级联子表
ALTER TABLE metrics_minute RENAME COLUMN app_id TO app_code;
ALTER TABLE clicks RENAME COLUMN app_id TO app_code;

-- 3b) slot_id -> slot_code（引用 ad_slots.code，uuid 转 text 保留原串）
ALTER TABLE decision_logs RENAME COLUMN slot_id TO slot_code;     -- 级联子表
ALTER TABLE decision_logs ALTER COLUMN slot_code TYPE text USING slot_code::text;
ALTER TABLE fill_priorities RENAME COLUMN slot_id TO slot_code;
ALTER TABLE fill_priorities ALTER COLUMN slot_code TYPE text USING slot_code::text;

-- 3c) admin_users.role -> role_code（引用 roles.code，text，保留默认值 'operator'）
ALTER TABLE admin_users RENAME COLUMN role TO role_code;

-- 3d) role_menus.role_code / menu_code 已是 *_code，值保持角色/菜单业务码，无需改动
-- 3e) menus.parent_code 已是该名，值保持

-- ============================================================
-- 4) 重建外键：关联列指向各维度表的 code 列（数据原样，类型匹配）
-- ============================================================
ALTER TABLE ad_slots ADD CONSTRAINT ad_slots_app_id_fkey FOREIGN KEY (app_code) REFERENCES ads_center.apps(code);
ALTER TABLE admin_users ADD CONSTRAINT admin_users_role_fk FOREIGN KEY (role_code) REFERENCES ads_center.roles(code);
ALTER TABLE budget_ledger ADD CONSTRAINT budget_ledger_app_id_fkey FOREIGN KEY (app_code) REFERENCES ads_center.apps(code);
ALTER TABLE decision_logs ADD CONSTRAINT decision_logs_slot_id_fkey FOREIGN KEY (slot_code) REFERENCES ads_center.ad_slots(code);
ALTER TABLE decision_logs ADD CONSTRAINT decision_logs_app_id_fkey FOREIGN KEY (app_code) REFERENCES ads_center.apps(code);
ALTER TABLE fill_priorities ADD CONSTRAINT fill_priorities_slot_id_fkey FOREIGN KEY (slot_code) REFERENCES ads_center.ad_slots(code);
ALTER TABLE menus ADD CONSTRAINT menus_parent_code_fkey FOREIGN KEY (parent_code) REFERENCES ads_center.menus(code);
ALTER TABLE metrics_minute ADD CONSTRAINT metrics_minute_app_id_fkey FOREIGN KEY (app_code) REFERENCES ads_center.apps(code);
ALTER TABLE role_menus ADD CONSTRAINT role_menus_role_code_fkey FOREIGN KEY (role_code) REFERENCES ads_center.roles(code);
ALTER TABLE role_menus ADD CONSTRAINT role_menus_menu_code_fkey FOREIGN KEY (menu_code) REFERENCES ads_center.menus(code);
ALTER TABLE ad_events ADD CONSTRAINT ad_events_app_id_fkey FOREIGN KEY (app_code) REFERENCES ads_center.apps(code);

-- ============================================================
-- 5) RPC 重建（落法 B：role_code 直接是角色码字符串；menu_code 直接是菜单码）
-- ============================================================
CREATE OR REPLACE FUNCTION public.current_admin_role()
RETURNS TEXT
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = ads_center, public
AS $$
    SELECT u.role_code
    FROM ads_center.admin_users u
    WHERE u.code = auth.uid()::text
      AND u.status = 'active';
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
                 JOIN ads_center.admin_users u ON u.role_code = rm.role_code
        WHERE u.code = auth.uid()::text
          AND u.status = 'active'
    ), tree AS (
        SELECT m.code AS menu_code, m.label, m.href, m.parent_code, m.sort_order
        FROM ads_center.menus m
        WHERE m.enabled
          AND m.code IN (SELECT menu_code FROM granted)
        UNION
        SELECT m.code, m.label, m.href, m.parent_code, m.sort_order
        FROM ads_center.menus m
                 JOIN tree t ON t.parent_code = m.code
        WHERE m.enabled
    )
    SELECT DISTINCT menu_code, label, href, parent_code, sort_order
    FROM tree
    ORDER BY parent_code NULLS FIRST, sort_order, menu_code;
$$;
REVOKE ALL ON FUNCTION public.current_admin_menus() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.current_admin_menus() TO authenticated, anon;
