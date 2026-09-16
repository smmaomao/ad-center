-- 000047_budget_ledger_agg_repair: 补回 000013 的聚合列与唯一索引（幂等）
--
-- 背景：部分库（本地 / 由 dump 恢复的库）虽在 schema_migrations 里记录了
-- 000013 已应用，但 budget_ledger 实际缺少 count 列与 budget_ledger_agg_uq
-- 部分唯一索引。消费端 WriteLedgerAgg 的
--   INSERT (..., count, ...) ... ON CONFLICT (advertiser_id, day, hour_bucket) WHERE op_type='deduct_agg'
-- 依赖二者，缺失时每批事件落库都会报错（连带同事务的写入一起失败）。
--
-- 本迁移幂等补回，对已正确迁移的库是无操作（IF NOT EXISTS）。
ALTER TABLE ads_center.budget_ledger
    ADD COLUMN IF NOT EXISTS count integer NOT NULL DEFAULT 1;

ALTER TABLE ads_center.budget_ledger DROP CONSTRAINT IF EXISTS budget_ledger_op_type_check;
ALTER TABLE ads_center.budget_ledger ADD CONSTRAINT budget_ledger_op_type_check
    CHECK (op_type = ANY (ARRAY['deduct', 'commit', 'rollback', 'calibrate', 'daily_reset', 'deduct_agg']));

CREATE UNIQUE INDEX IF NOT EXISTS budget_ledger_agg_uq
    ON ads_center.budget_ledger (advertiser_id, day, hour_bucket)
    WHERE op_type = 'deduct_agg';
