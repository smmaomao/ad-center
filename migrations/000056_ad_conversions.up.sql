-- V1.4：把 S2S 转化回传事件从 ad_events 拆分到独立表 ad_conversions。
--
-- 背景：原先 install/activate/register/first_purchase/purchase 等转化回传与内部投放
-- 事件（request/fill/impression/click/video_complete）混写在分区表 ad_events（审计回溯
-- 用途）。转化来自外部归因方/中介(berealads)，低频且用于对账/按任务算 ROI，与内部
-- 高频投放事件查询模式不同，混表会无谓膨胀审计表。
--
-- 拆分后：
--   - ad_events 回归纯内部投放事件；
--   - 转化落 ad_conversions，按 campaign_id/advertiser_id/pixel_id/click_id 建索引，
--     便于按广告任务与中介标记对账统计；
--   - 计费流水(budget_ledger)与广告主钱包(advertisers.wallet_balance)仍由
--     WriteEventBatch 同事务更新（与明细表解耦，见 store_write.go），对账不受影响。
--   - metrics_minute 的转化计数由 metrics.RecordEvent 独立聚合，不经过明细表，故无影响。
--
-- ad_events 上 000055 加的 campaign_id/pixel_id 列保留（内部事件审计可能使用），无害。
-- 转化量相对曝光小，ad_conversions 不按月分区，普通表 + 索引即可。

CREATE TABLE ads_center.ad_conversions (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    click_id      TEXT NOT NULL,                       -- S2S 回传的 clickid，归因反查键
    app_code      TEXT NOT NULL,
    advertiser_id BIGINT,                              -- 归因到的广告主（click 反查）
    campaign_id   TEXT,                                -- 服务端权威任务归属（creative→campaign）
    creative_id   TEXT,
    device_id     TEXT,
    style         TEXT,
    event_type    TEXT NOT NULL
                      CHECK (event_type IN ('install', 'activate', 'register', 'first_purchase', 'purchase')),
    revenue       FLOAT8 NOT NULL DEFAULT 0,           -- 服务端确认的实际扣费（CPA 单价），不信回传
    pixel_id      TEXT,                                -- 中介/berealads 回传的任务标记，便于对照
    currency      TEXT,                                -- 充值事件币种(ISO 4217)，仅记录
    value         FLOAT8,                              -- 充值事件流水金额，仅记录不参与计费
    ts            TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_ad_conversions_campaign_ts ON ads_center.ad_conversions (campaign_id, ts);
CREATE INDEX idx_ad_conversions_adv_ts      ON ads_center.ad_conversions (advertiser_id, ts);
CREATE INDEX idx_ad_conversions_click_id    ON ads_center.ad_conversions (click_id);
