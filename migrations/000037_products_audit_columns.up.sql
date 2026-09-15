-- products 补 created_by / updated_by：与 advertisers / campaigns / creatives 一致的操作者审计列。
-- 此前 products 表缺少这两列，导致 CreateProduct / UpdateProduct 因插入不存在的列而失败。
-- 非破坏性：仅 ADD COLUMN（TEXT，可空，历史行留空）。
ALTER TABLE ads_center.products ADD COLUMN created_by TEXT;
ALTER TABLE ads_center.products ADD COLUMN updated_by TEXT;
COMMENT ON COLUMN ads_center.products.created_by IS '创建者（审计）';
COMMENT ON COLUMN ads_center.products.updated_by IS '最后操作者（审计）';
