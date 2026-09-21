-- 000057: campaigns.billing_mode CHECK 对齐代码侧业务枚举。
--
-- 背景（BUG）：config.Campaign.BillingAmount 与后台表单（internal/api/
-- admin_campaigns.go 的 campaignBillingModes）已支持 7 种扣费方式：
--   cpm / cpc / cpi / cpa-activate / cpa-register / cpa-first-deposit / cpa-pay
-- 各自对应一个计费事件（impression / click / install / activate / register /
-- first_purchase / purchase）。
--
-- 但 000050 放宽后的 CHECK 仍只接纳 4 个取值：
--   ('cpm', 'cpc', 'cpa', 'cpa-activate')
-- 于是保存广告任务时选中 cpi / cpa-register / cpa-first-deposit / cpa-pay
-- 会被约束拦下并报错：
--   ERROR: new row for relation "campaigns" violates check constraint
--          "campaigns_billing_mode_check" (SQLSTATE 23514)
--
-- 修复：放开到全部业务取值，与代码枚举保持一致。'cpa' 为历史遗留取值
-- （000050 已把存量 'cpa' 回落到 'cpa-activate'），此处保留以免残留行在
-- 被 UPDATE 时因子集不含它而再次触发约束失败。

ALTER TABLE ads_center.campaigns
    DROP CONSTRAINT IF EXISTS campaigns_billing_mode_check,
    ADD CONSTRAINT campaigns_billing_mode_check CHECK (
        billing_mode = ANY (ARRAY[
            'cpm'::text, 'cpc'::text, 'cpi'::text,
            'cpa'::text, 'cpa-activate'::text, 'cpa-register'::text,
            'cpa-first-deposit'::text, 'cpa-pay'::text
        ])
    );
