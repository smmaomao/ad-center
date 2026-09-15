-- V1.3：点击归因登记表
--
-- 投放方（我们的 App）在用户点击广告、跳转广告方落地页前，生成唯一 clickid
-- 并登记本次点击的归属上下文（app/slot/advertiser/device/creative）。广告方
-- （对接 Adjust / H5 / PWA）把 clickid 当"用户标识"存下，后续转化事件经 S2S
-- 回传（GET /v1/s2s/event?clickid=...）时，服务端凭 clickid 反查本表还原归属
-- 并扣费。归属只信服务端自己的点击登记，不信回调自报归属参数（防伪造归因）。

CREATE TABLE ads_center.clicks (
    click_id      TEXT PRIMARY KEY,
    app_id        TEXT NOT NULL,
    advertiser_id TEXT NOT NULL,
    slot_id       TEXT NOT NULL,
    device_id     TEXT,
    creative_id   TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- created_at 索引：归因窗口过期清理（> ClickTTL=30d）+ 按时间对账。
CREATE INDEX clicks_created_at_idx ON ads_center.clicks (created_at);
-- 按 app/广告主维度对账查询。
CREATE INDEX clicks_app_advertiser_idx ON ads_center.clicks (app_id, advertiser_id);
