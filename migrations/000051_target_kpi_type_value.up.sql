-- 000051: 目标 KPI 由单一 target_cpi 拆为两字段
--   target_kpi_type：KPI 指标类型（与 billing_mode 同枚举，通常与其一致）
--   target_kpi_value：目标 KPI 值（如目标 CPI=$1.80）
-- 历史 cpa 任务的目标本是「目标 CPI」，故旧值回落到 type='cpi'。
ALTER TABLE ads_center.campaigns
    RENAME COLUMN target_cpi TO target_kpi_value;

ALTER TABLE ads_center.campaigns
    ADD COLUMN target_kpi_type text NOT NULL DEFAULT 'cpi';
