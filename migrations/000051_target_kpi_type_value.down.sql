-- 回滚 000051
ALTER TABLE ads_center.campaigns
    DROP COLUMN target_kpi_type;

ALTER TABLE ads_center.campaigns
    RENAME COLUMN target_kpi_value TO target_cpi;
