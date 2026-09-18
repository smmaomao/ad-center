-- 000053_advertiser_wallet_adjust: 广告主钱包手动调账流水
--
-- 背景：充值是"进项"（advertiser_recharges.amount > 0），但后台偶尔需要直接
-- 修正余额（冲正、补差、迁移对不上）。这类操作不并入"累计充值"，但必须留痕，
-- 且余额随之变动。
--
-- 与充值分表的原因：调账额可正可负，且语义 ≠ 充值（不计入累计充值），
-- 后台合并流水时以 kind=adjust 单独展示。
CREATE TABLE IF NOT EXISTS ads_center.advertiser_wallet_adjustments (
    id            bigserial PRIMARY KEY,
    advertiser_id bigint NOT NULL REFERENCES ads_center.advertisers(id) ON DELETE CASCADE,
    amount        numeric(18,6) NOT NULL, -- 实际变动额：正=增加，负=减少
    note          text,
    created_by    text,
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_advertiser_wallet_adjustments_adv
    ON ads_center.advertiser_wallet_adjustments (advertiser_id, created_at DESC);
