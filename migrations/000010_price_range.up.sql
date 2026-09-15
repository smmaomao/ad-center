-- V1.4：计费单价区间化（每次扣费在区间内均匀随机）
--
-- 1) cpm/cpc 增加 bidding_price_min（区间下限）。默认 0 = 未配置下限，
--    计费退化为固定 BiddingPrice（与旧行为一致，向后兼容）；配置下限后即在
--    [bidding_price_min, bidding_price] 内随机扣费。
-- 2) cpa 的 cpa_event_prices 由「事件 → 单值」改为「事件 → [min,max] 数组」。
--    历史单值数据在此迁移中就地转成 [v, v]（退化为固定价，向后兼容）。
--    BFF / 种子数据写入时也需改为数组格式：{"install":[1.0,2.0]}。

ALTER TABLE ads_center.advertisers
    ADD COLUMN IF NOT EXISTS bidding_price_min float8 NOT NULL DEFAULT 0;

UPDATE ads_center.advertisers
SET cpa_event_prices = (
    SELECT COALESCE(jsonb_object_agg(k, jsonb_build_array(v, v)), '{}'::jsonb)
    FROM jsonb_each(cpa_event_prices) AS t(k, v)
)
WHERE cpa_event_prices IS NOT NULL AND cpa_event_prices <> '{}'::jsonb;
