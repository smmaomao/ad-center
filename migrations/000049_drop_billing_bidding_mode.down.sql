-- 回滚 000049（仅恢复列结构，不恢复数据）
ALTER TABLE ads_center.campaigns
    ADD COLUMN IF NOT EXISTS bidding_mode text NOT NULL DEFAULT 'cpi';

ALTER TABLE ads_center.advertisers
    ADD COLUMN IF NOT EXISTS bidding_price numeric(12,6) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS bidding_price_min float8 NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS billing_mode text NOT NULL DEFAULT 'cpa',
    ADD COLUMN IF NOT EXISTS cpa_event_prices jsonb NOT NULL DEFAULT '{}'::jsonb;
