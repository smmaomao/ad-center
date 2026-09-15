-- 广告主级 今日预算 / 今日已耗 已上移至 campaign（广告任务）粒度。
-- 预算闸改为由各 campaign.daily_budget 各自控制（PRD/讨论 2026-09）。
ALTER TABLE ads_center.advertisers
  DROP COLUMN IF EXISTS daily_budget,
  DROP COLUMN IF EXISTS spent_today;
