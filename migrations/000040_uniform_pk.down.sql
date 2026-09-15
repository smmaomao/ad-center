-- 000040_uniform_pk down：回滚统一主键改名

ALTER TABLE decision_logs DROP CONSTRAINT decision_logs_pkey;
ALTER TABLE decision_logs DROP CONSTRAINT uq_decision_logs_bk;
ALTER TABLE decision_logs DROP COLUMN id;
ALTER TABLE decision_logs ADD PRIMARY KEY (decision_id, ts);
DROP SEQUENCE IF EXISTS decision_logs_id_seq;

ALTER TABLE role_menus DROP CONSTRAINT role_menus_pkey;
ALTER TABLE role_menus DROP CONSTRAINT uq_role_menus_bk;
ALTER TABLE role_menus DROP COLUMN id;
ALTER TABLE role_menus ADD PRIMARY KEY (role_code, menu_code);
DROP SEQUENCE IF EXISTS role_menus_id_seq;

ALTER TABLE metrics_minute DROP CONSTRAINT metrics_minute_pkey;
ALTER TABLE metrics_minute DROP CONSTRAINT uq_metrics_minute_bk;
ALTER TABLE metrics_minute DROP COLUMN id;
ALTER TABLE metrics_minute ADD PRIMARY KEY (style, advertiser_id, minute_ts);
DROP SEQUENCE IF EXISTS metrics_minute_id_seq;

-- 列改名回滚（外键约束自动跟随）
ALTER TABLE products    RENAME COLUMN id TO product_id;
ALTER TABLE campaigns   RENAME COLUMN id TO campaign_id;
ALTER TABLE creatives   RENAME COLUMN id TO creative_id;
ALTER TABLE advertisers RENAME COLUMN id TO advertiser_id;
