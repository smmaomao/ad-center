-- V1.2：计费模型（扣费锚点由计费方式决定）
--
-- 之前模型的问题：req 下发即按 bidding_price 全价"预扣"（engine.materialize
-- TryDeduct）。bidding_price 是单次安装/行动的价，把它当作每次展示的价格预扣，
-- 预算会以几十~上百倍速度虚假耗尽；且 fill（下发）≠ 真实曝光，未曝光就扣钱
-- 账目也是错的。
--
-- 新模型：
--   req        只下发 + 记 fill，预算降级为只读闸（spent >= budget 拒发），不扣钱
--   impression 客户端回执 → billing_mode='cpm' 时扣费（单次 = billing_price/1000）
--   click      客户端回执 → billing_mode='cpc' 时扣费（单次 = billing_price）
--   S2S 回调   归因方（Adjust/广告主）通知 → billing_mode='cpa' 时按具体转化事件扣费
--     （install/activate/register/first_purchase/purchase，单价配置在 cpa_event_prices）
--
-- KPI 考核口径（bidding_mode/target_cpi/actual_cpi）与计费方式（billing_mode）
-- 是两回事：bidding_mode 决定"如何考核效果"，billing_mode 决定"何时扣广告主的钱"。

ALTER TABLE ads_center.advertisers
    ADD COLUMN billing_mode TEXT NOT NULL DEFAULT 'cpa'
        CHECK (billing_mode IN ('cpm', 'cpc', 'cpa')),
    ADD COLUMN cpa_event_prices JSONB NOT NULL DEFAULT '{}'::jsonb;
COMMENT ON COLUMN ads_center.advertisers.billing_mode IS
    '计费方式（决定扣费锚点）：cpm=客户端曝光回执扣费、cpc=客户端点击回执扣费、cpa=归因方S2S转化回调扣费；KPI考核口径仍看 bidding_mode/target_cpi';
COMMENT ON COLUMN ads_center.advertisers.cpa_event_prices IS
    'billing_mode=cpa 时各转化事件的单价（美元），如 {"install":1.0,"activate":0.5}；事件不在 map 中则不扣费';

-- 存量数据迁移：cpi/cpa 出价口径按安装结算 → billing cpa；revenue_share 未实装，保守按 cpm（曝光计费）
UPDATE ads_center.advertisers
SET billing_mode = CASE
        WHEN bidding_mode IN ('cpi', 'cpa') THEN 'cpa'
        ELSE 'cpm' END,
    cpa_event_prices = jsonb_build_object('install', bidding_price)
WHERE deleted_at IS NULL;

-- 客户端事件通道不再接受 conversion（转化归因走 S2S，见 /v1/s2s/event），
-- 转化事件细分到具体动作以便按事件计费（cpa_event_prices 的 key）
ALTER TABLE ads_center.ad_events
    DROP CONSTRAINT ad_events_event_type_check,
    ADD CONSTRAINT ad_events_event_type_check CHECK (event_type IN
        ('request', 'fill', 'impression', 'click', 'conversion',
         'install', 'activate', 'register', 'first_purchase', 'purchase'));
