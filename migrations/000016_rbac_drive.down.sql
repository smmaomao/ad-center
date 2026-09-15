DROP FUNCTION IF EXISTS public.current_admin_menus();

-- 恢复 000001 的角色枚举约束（若已分配自定义角色，此步会失败，需先清理）
ALTER TABLE ads_center.admin_users
    DROP CONSTRAINT IF EXISTS admin_users_role_fk;
ALTER TABLE ads_center.admin_users
    DROP CONSTRAINT IF EXISTS admin_users_role_check;
ALTER TABLE ads_center.admin_users
    ADD CONSTRAINT admin_users_role_check
        CHECK (role IN ('super_admin', 'operator', 'analyst', 'strategy'));
