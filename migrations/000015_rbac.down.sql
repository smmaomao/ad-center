DROP TRIGGER IF EXISTS trg_menus_touch ON ads_center.menus;
DROP TRIGGER IF EXISTS trg_roles_touch ON ads_center.roles;
ALTER TABLE ads_center.admin_users DROP COLUMN IF EXISTS status;
DROP TABLE IF EXISTS ads_center.role_menus;
DROP TABLE IF EXISTS ads_center.menus;
DROP TABLE IF EXISTS ads_center.roles;
