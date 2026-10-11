-- 广告任务统计时区：数据分析按天聚合以此为准（IANA 时区名，如 Asia/Bangkok）。
-- 之前统计默认按 UTC 切天，跨时区（如 UTC+7）会把"一天"算错，导致与落地页统计对不上。
ALTER TABLE ads_center.campaigns
  ADD COLUMN IF NOT EXISTS timezone TEXT NOT NULL DEFAULT 'UTC';

COMMENT ON COLUMN ads_center.campaigns.timezone IS
  '广告任务统计时区（IANA 名称，如 Asia/Bangkok / Asia/Shanghai / UTC）。数据分析按天聚合时使用，避免跨时区把一天算错。';
