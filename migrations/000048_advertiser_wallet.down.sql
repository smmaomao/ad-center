-- 回滚 000048_advertiser_wallet
DROP TABLE IF EXISTS ads_center.advertiser_recharges;

ALTER TABLE ads_center.advertisers
    DROP COLUMN IF EXISTS wallet_balance,
    DROP COLUMN IF EXISTS wallet_enabled;
