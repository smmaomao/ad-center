DROP INDEX IF EXISTS budget_ledger_agg_uq;

ALTER TABLE budget_ledger DROP COLUMN IF EXISTS count;

ALTER TABLE budget_ledger DROP CONSTRAINT IF EXISTS budget_ledger_op_type_check;
ALTER TABLE budget_ledger ADD CONSTRAINT budget_ledger_op_type_check
  CHECK (op_type = ANY (ARRAY['deduct', 'commit', 'rollback', 'calibrate', 'daily_reset']));
