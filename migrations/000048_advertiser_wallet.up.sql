-- 000048_advertiser_wallet: 广告主总钱包（充值 - 扣费），余额为投放硬顶
--
-- 背景：campaign/product 的 daily_budget 只是子限额与消耗节奏控制；广告主需要
-- 一个"总余额"硬顶——即使当日预算没超，总余额耗尽也必须停投该广告主下全部
-- campaign。
--
-- 余额归属：广告主（account 级）。充值是账户行为（公司打款），钱包跨产品共享。
--
-- 向后兼容（重要）：存量广告主没有钱包概念，wallet_enabled 默认 false →
-- 不进钱包表、不被钱包闸限制，行为与上线前完全一致。首次充值自动置
-- wallet_enabled = true，此后钱包闸生效（余额 <= 0 停投）。
ALTER TABLE ads_center.advertisers
    ADD COLUMN IF NOT EXISTS wallet_enabled boolean NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS wallet_balance numeric(18,6) NOT NULL DEFAULT 0;

COMMENT ON COLUMN ads_center.advertisers.wallet_enabled IS
    '是否启用总钱包闸：false=不受总余额限制（存量广告主默认），首次充值置 true';
COMMENT ON COLUMN ads_center.advertisers.wallet_balance IS
    '总余额 = 累计充值 - 累计扣费；启用后 <=0 停投该广告主下全部 campaign';

-- 充值流水（钱包进项；扣费流水复用既有 budget_ledger，后台合并两表展示）
CREATE TABLE IF NOT EXISTS ads_center.advertiser_recharges (
    id            bigserial PRIMARY KEY,
    advertiser_id bigint NOT NULL REFERENCES ads_center.advertisers(id) ON DELETE CASCADE,
    amount        numeric(18,6) NOT NULL CHECK (amount > 0),
    currency      text NOT NULL DEFAULT 'USD',
    note          text,
    created_by    text,
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_advertiser_recharges_adv
    ON ads_center.advertiser_recharges (advertiser_id, created_at DESC);
