-- 回滚 000047_budget_ledger_agg_repair
DROP INDEX IF EXISTS ads_center.budget_ledger_agg_uq;
ALTER TABLE ads_center.budget_ledger DROP COLUMN IF EXISTS count;
