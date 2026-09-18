-- 000049: 职责收敛 —— 计费 / 出价一律在广告任务（campaign）级，广告主只管钱包。
--
--   1) campaigns.bidding_mode（"出价口径"）废弃：系统只保留 billing_mode 一个口径，
--      「出价」即该计费方式的单价（BiddingPrice 上限 + BiddingPriceMin 下限区间）。
--   2) advertisers 的计费字段整体移除（出价 / 计费方式 / CPA 单价）：这些字段已
--      不再参与任何排序或扣费（扣费改由 campaigns 的同名列决定，数据已存在）。
ALTER TABLE ads_center.campaigns DROP COLUMN IF EXISTS bidding_mode;

ALTER TABLE ads_center.advertisers
    DROP COLUMN IF EXISTS bidding_price,
    DROP COLUMN IF EXISTS bidding_price_min,
    DROP COLUMN IF EXISTS billing_mode,
    DROP COLUMN IF EXISTS cpa_event_prices;
