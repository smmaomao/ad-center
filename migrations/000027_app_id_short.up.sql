-- 应用 ID 由 UUID 改为短字符串（10 位 = 数字 + 小写字母），
-- 缩短后台展示与客户端传参长度；全局唯一仍由 apps.app_id 主键约束保证。
--
-- app_id 被多张表（含 ad_events / decision_logs 的按月分区子表）当外键引用，
-- FK 两侧类型必须一致，故需：① 删除所有引用 apps.app_id 的外键（含分区表）
-- ② 6 张表统一改 TEXT ③ 重建外键（仅父表，分区自动继承）。
--
-- 用 DO 块包裹，仅当 app_id 仍为 uuid 时执行，保证可重复运行（幂等）。

DO $$
DECLARE r RECORD;
BEGIN
  IF EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_schema='ads_center' AND table_name='apps'
      AND column_name='app_id' AND data_type='uuid'
  ) THEN
    -- 1) 删除所有引用 apps.app_id 的外键（父表 + 分区子表）。逐条忽略单条失败，继续。
    FOR r IN
      SELECT conname, conrelid::regclass::text AS child
      FROM pg_constraint
      WHERE contype='f' AND confrelid = 'ads_center.apps'::regclass
    LOOP
      BEGIN
        EXECUTE format('ALTER TABLE %s DROP CONSTRAINT IF EXISTS %I', r.child, r.conname);
      EXCEPTION WHEN others THEN
        NULL;
      END;
    END LOOP;

    -- 2) 改类型：父表 ALTER 自动级联到分区；其余表单独改。apps 去掉 UUID 默认值。
    ALTER TABLE ads_center.apps           ALTER COLUMN app_id TYPE TEXT USING app_id::text, ALTER COLUMN app_id DROP DEFAULT;
    ALTER TABLE ads_center.ad_slots       ALTER COLUMN app_id TYPE TEXT USING app_id::text;
    ALTER TABLE ads_center.ad_events      ALTER COLUMN app_id TYPE TEXT USING app_id::text;
    ALTER TABLE ads_center.metrics_minute ALTER COLUMN app_id TYPE TEXT USING app_id::text;
    ALTER TABLE ads_center.decision_logs  ALTER COLUMN app_id TYPE TEXT USING app_id::text;
    ALTER TABLE ads_center.budget_ledger  ALTER COLUMN app_id TYPE TEXT USING app_id::text;

    -- 3) 重建外键（仅父表，分区自动继承）；NO ACTION 与原始定义一致。
    ALTER TABLE ads_center.ad_slots       ADD CONSTRAINT ad_slots_app_id_fkey       FOREIGN KEY (app_id) REFERENCES ads_center.apps(app_id);
    ALTER TABLE ads_center.ad_events      ADD CONSTRAINT ad_events_app_id_fkey      FOREIGN KEY (app_id) REFERENCES ads_center.apps(app_id);
    ALTER TABLE ads_center.metrics_minute ADD CONSTRAINT metrics_minute_app_id_fkey FOREIGN KEY (app_id) REFERENCES ads_center.apps(app_id);
    ALTER TABLE ads_center.decision_logs  ADD CONSTRAINT decision_logs_app_id_fkey  FOREIGN KEY (app_id) REFERENCES ads_center.apps(app_id);
    ALTER TABLE ads_center.budget_ledger  ADD CONSTRAINT budget_ledger_app_id_fkey  FOREIGN KEY (app_id) REFERENCES ads_center.apps(app_id);
  END IF;
END $$;
