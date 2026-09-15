ALTER TABLE ads_center.campaigns DROP COLUMN IF EXISTS product_id;
DROP TABLE IF EXISTS ads_center.products;
DELETE FROM ads_center.role_menus WHERE menu_code = 'products';
DELETE FROM ads_center.menus WHERE code = 'products';
