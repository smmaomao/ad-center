-- 000052: 移除 campaigns.actual_cpi 列
-- 实测 CPI 不再落库：仅存目标 target_kpi_type / target_kpi_value，
-- 后期自动优化时再按需以运行时 metrics（消耗/转化）计算实测值。
ALTER TABLE ads_center.campaigns
    DROP COLUMN IF EXISTS actual_cpi;
