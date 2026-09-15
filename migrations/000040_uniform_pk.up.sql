-- 000040_uniform_pk: 统一主键命名为 id（bigint）
--
-- 目标（来自需求澄清的三项决定）：
--   1) PK 类型统一为 bigint（64 位）
--   2) 语义 bigint 主键改名：advertiser_id / creative_id / campaign_id / product_id → id
--   3) 复合 / 分区表加代理 id 列，原键降为 UNIQUE 约束
--
-- 注意：ALTER TABLE ... RENAME COLUMN 会自动更新引用该列的外键约束
--      （含 ad_events / decision_logs 各分区上的 FK），无需手工重建。

-- 1) 语义 bigint 主键改名（默认值 nextval(...) 随列保留，insert 不受影响）
ALTER TABLE advertisers RENAME COLUMN advertiser_id TO id;
ALTER TABLE creatives  RENAME COLUMN creative_id  TO id;
ALTER TABLE campaigns  RENAME COLUMN campaign_id  TO id;
ALTER TABLE products    RENAME COLUMN product_id   TO id;

-- 2) metrics_minute：加代理 id 列，原复合主键 (style, advertiser_id, minute_ts) 降为唯一约束
CREATE SEQUENCE IF NOT EXISTS metrics_minute_id_seq;
ALTER TABLE metrics_minute ADD COLUMN id bigint;
ALTER TABLE metrics_minute ALTER COLUMN id SET DEFAULT nextval('metrics_minute_id_seq');
UPDATE metrics_minute SET id = nextval('metrics_minute_id_seq');
ALTER TABLE metrics_minute ALTER COLUMN id SET NOT NULL;
ALTER TABLE metrics_minute DROP CONSTRAINT metrics_minute_pkey;
ALTER TABLE metrics_minute ADD PRIMARY KEY (id);
ALTER TABLE metrics_minute ADD CONSTRAINT uq_metrics_minute_bk UNIQUE (style, advertiser_id, minute_ts);

-- 3) role_menus：加代理 id 列，原复合主键 (role_code, menu_code) 降为唯一约束
CREATE SEQUENCE IF NOT EXISTS role_menus_id_seq;
ALTER TABLE role_menus ADD COLUMN id bigint;
ALTER TABLE role_menus ALTER COLUMN id SET DEFAULT nextval('role_menus_id_seq');
UPDATE role_menus SET id = nextval('role_menus_id_seq');
ALTER TABLE role_menus ALTER COLUMN id SET NOT NULL;
ALTER TABLE role_menus DROP CONSTRAINT role_menus_pkey;
ALTER TABLE role_menus ADD PRIMARY KEY (id);
ALTER TABLE role_menus ADD CONSTRAINT uq_role_menus_bk UNIQUE (role_code, menu_code);

-- 4) decision_logs（分区表）：加代理 id 列，原 (decision_id, ts) 降为唯一约束，PK 改为 (id, ts)
CREATE SEQUENCE IF NOT EXISTS decision_logs_id_seq;
ALTER TABLE decision_logs ADD COLUMN id bigint;
ALTER TABLE decision_logs ALTER COLUMN id SET DEFAULT nextval('decision_logs_id_seq');
UPDATE decision_logs SET id = nextval('decision_logs_id_seq');
ALTER TABLE decision_logs ALTER COLUMN id SET NOT NULL;
ALTER TABLE decision_logs DROP CONSTRAINT decision_logs_pkey;
ALTER TABLE decision_logs ADD PRIMARY KEY (id, ts);
ALTER TABLE decision_logs ADD CONSTRAINT uq_decision_logs_bk UNIQUE (decision_id, ts);
