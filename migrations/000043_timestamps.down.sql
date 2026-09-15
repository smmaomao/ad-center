-- 000043 down: 删除 000043 新增的 created_at / updated_at 列
ALTER TABLE metrics_minute DROP COLUMN IF EXISTS created_at;
ALTER TABLE metrics_minute DROP COLUMN IF EXISTS updated_at;
ALTER TABLE role_menus DROP COLUMN IF EXISTS created_at;
ALTER TABLE role_menus DROP COLUMN IF EXISTS updated_at;
ALTER TABLE settings DROP COLUMN IF EXISTS created_at;
ALTER TABLE clicks DROP COLUMN IF EXISTS updated_at;
ALTER TABLE audit_logs DROP COLUMN IF EXISTS updated_at;
ALTER TABLE budget_ledger DROP COLUMN IF EXISTS updated_at;
ALTER TABLE ad_events DROP COLUMN IF EXISTS created_at;
ALTER TABLE ad_events DROP COLUMN IF EXISTS updated_at;
ALTER TABLE decision_logs DROP COLUMN IF EXISTS created_at;
ALTER TABLE decision_logs DROP COLUMN IF EXISTS updated_at;
