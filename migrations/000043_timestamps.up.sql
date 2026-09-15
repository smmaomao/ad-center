-- 000043: 为缺失 created_at / updated_at 的表补齐（含分区父表，自动级联子表）
-- 统一 timestamptz NOT NULL DEFAULT now()，存量行由 PG 自动回填。

-- metrics_minute：两者都缺
ALTER TABLE metrics_minute ADD COLUMN created_at timestamptz NOT NULL DEFAULT now();
ALTER TABLE metrics_minute ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now();

-- role_menus：两者都缺
ALTER TABLE role_menus ADD COLUMN created_at timestamptz NOT NULL DEFAULT now();
ALTER TABLE role_menus ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now();

-- settings：缺 created_at（已有 updated_at）
ALTER TABLE settings ADD COLUMN created_at timestamptz NOT NULL DEFAULT now();

-- clicks：缺 updated_at（已有 created_at）
ALTER TABLE clicks ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now();

-- audit_logs：缺 updated_at（已有 created_at）
ALTER TABLE audit_logs ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now();

-- budget_ledger：缺 updated_at（已有 created_at）
ALTER TABLE budget_ledger ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now();

-- ad_events（分区表）：两者都缺，父表加即级联所有子表
ALTER TABLE ad_events ADD COLUMN created_at timestamptz NOT NULL DEFAULT now();
ALTER TABLE ad_events ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now();

-- decision_logs（分区表）：两者都缺
ALTER TABLE decision_logs ADD COLUMN created_at timestamptz NOT NULL DEFAULT now();
ALTER TABLE decision_logs ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now();
