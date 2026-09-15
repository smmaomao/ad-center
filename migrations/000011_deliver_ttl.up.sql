-- V1.5：每条广告的下发展示有效期（客户端展示缓存 TTL）
--
-- 广告下发后，客户端按响应里的 expire_at（Unix 秒）判断是否过期：
-- 超过过期时间则不再展示该广告，等下次请求时重新拉取最新列表。
-- 该 TTL 是「每条广告（广告主）维度」的配置，不配置（0）时兜底 10 分钟。

ALTER TABLE ads_center.advertisers
    ADD COLUMN IF NOT EXISTS deliver_ttl_minutes integer NOT NULL DEFAULT 10;
COMMENT ON COLUMN ads_center.advertisers.deliver_ttl_minutes IS
    '客户端展示有效期（分钟）：广告下发后客户端据此判断是否过期隐藏并重新拉取；0/未配置兜底 10 分钟';
