-- 点击登记固化归属任务：点击发生时就记下 campaign_id。
--
-- 背景：S2S 转化回调此前靠「素材 → 任务」运行时快照反查归因
-- （snap.CreativeCampaign），而快照只加载 status = 'active' 的任务。任务暂停或
-- 删除后反查落空，转化虽写进 ad_conversions 却没有归属任务——看板统计不到，
-- 也无法按任务核对与落地页系统的差异。
-- 固化后归属只信服务端自己的点击登记，与广告任务当前状态无关。
ALTER TABLE ads_center.clicks
  ADD COLUMN IF NOT EXISTS campaign_id TEXT;

COMMENT ON COLUMN ads_center.clicks.campaign_id IS
  '点击发生时的归属广告任务（取自下发 bid 上下文）。S2S 转化回调据此归因，不依赖运行时快照。';

CREATE INDEX IF NOT EXISTS idx_clicks_campaign_id ON ads_center.clicks (campaign_id);
