-- 回滚：cpa 区间 [min,max] 取 max 退化回单值；移除 bidding_price_min 列。
UPDATE ads_center.advertisers
SET cpa_event_prices = (
    SELECT COALESCE(jsonb_object_agg(k, (v->>1)::float8), '{}'::jsonb)
    FROM jsonb_each(cpa_event_prices) AS t(k, v)
)
WHERE cpa_event_prices IS NOT NULL AND cpa_event_prices <> '{}'::jsonb;

ALTER TABLE ads_center.advertisers DROP COLUMN bidding_price_min;
