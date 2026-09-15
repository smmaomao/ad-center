-- M2：计费流水聚合写（SCALING.md §2 / §7）
--
-- 背景：扣费流水原本每事件一行（且写在请求路径上），曝光量一大就是写放大。
-- 改为消费端按 (广告主, 小时) 聚合后一行写入：
--   - count 列记录该聚合行累计了多少次扣费
--   - 为聚合行（op_type='deduct_agg'）建**部分唯一索引**，配合
--     INSERT ... ON CONFLICT DO UPDATE 累加 —— 使"至少一次"投递天然幂等，
--     重复消费同一批消息不会把金额算两遍
--
-- 历史明细行（op_type='deduct' 等）不受该唯一索引约束，原样保留用于追溯。
ALTER TABLE budget_ledger ADD COLUMN IF NOT EXISTS count integer NOT NULL DEFAULT 1;

-- op_type 检查约束放开聚合行类型
ALTER TABLE budget_ledger DROP CONSTRAINT IF EXISTS budget_ledger_op_type_check;
ALTER TABLE budget_ledger ADD CONSTRAINT budget_ledger_op_type_check
  CHECK (op_type = ANY (ARRAY['deduct', 'commit', 'rollback', 'calibrate', 'daily_reset', 'deduct_agg']));

CREATE UNIQUE INDEX IF NOT EXISTS budget_ledger_agg_uq
  ON budget_ledger (advertiser_id, day, hour_bucket)
  WHERE op_type = 'deduct_agg';
