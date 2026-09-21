-- 回滚 000057：恢复到 000050 放宽后的 4 个取值
-- （'cpm', 'cpc', 'cpa', 'cpa-activate'）。
-- 注意：若库中已存有 cpi / cpa-register / cpa-first-deposit / cpa-pay 的行，
-- 此回滚会让这些行在后续 UPDATE 时再次触发约束失败，需先把数据回落再回滚。
ALTER TABLE ads_center.campaigns
    DROP CONSTRAINT IF EXISTS campaigns_billing_mode_check,
    ADD CONSTRAINT campaigns_billing_mode_check CHECK (
        billing_mode = ANY (ARRAY[
            'cpm'::text, 'cpc'::text, 'cpa'::text, 'cpa-activate'::text
        ])
    );
