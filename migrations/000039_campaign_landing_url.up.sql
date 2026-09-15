-- 广告任务落地页：客户端接口（docs/客户端接口.md）下发的 click_url 来源。
-- 客户端点击时把 click_url 中的 {CLICK_ID} 占位符替换为服务端下发的 click_id 后跳转。
ALTER TABLE ads_center.campaigns ADD COLUMN IF NOT EXISTS landing_url TEXT;

COMMENT ON COLUMN ads_center.campaigns.landing_url IS
'广告任务落地页 URL，客户端接口下发 click_url 的来源；{CLICK_ID} 由客户端替换为服务端 click_id';
