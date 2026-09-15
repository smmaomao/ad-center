-- 回滚 000035.up：把 KPI / 排期 / 下发有效期字段归还到 advertiser 维度。

ALTER TABLE ads_center.campaigns
  DROP COLUMN IF EXISTS deliver_ttl_minutes;

ALTER TABLE ads_center.advertisers
  ADD COLUMN IF NOT EXISTS end_at timestamptz,
  ADD COLUMN IF NOT EXISTS target_cpi double precision NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS actual_cpi double precision NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS consume_speed text NOT NULL DEFAULT 'even'
      CHECK (consume_speed IN ('even', 'accelerated', 'asap')),
  ADD COLUMN IF NOT EXISTS bidding_mode text NOT NULL DEFAULT 'cpi'
      CHECK (bidding_mode IN ('cpi', 'cpa', 'revenue_share')),
  ADD COLUMN IF NOT EXISTS deliver_ttl_minutes integer NOT NULL DEFAULT 10;
