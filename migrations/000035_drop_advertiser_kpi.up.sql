-- 广告主回归「身份 + 计费锚点」：运行期 KPI / 排期 / 下发有效期统一在
-- 广告任务（campaign）维度配置。
--   - migration 000026 已把 target_cpi / bidding_mode(KPI) / start_at / end_at 加到 campaigns；
--   - migration 000031 已把 actual_cpi / spent_today / consume_speed / guaranteed_* / priority_score 迁到 campaigns；
-- 本迁移补齐剩余字段：从 advertisers 移除 end_at / target_cpi / actual_cpi /
-- consume_speed / bidding_mode / deliver_ttl_minutes，并给 campaigns 补 deliver_ttl_minutes
-- （引擎按 campaign 读取下发有效期，store_campaigns 的 COALESCE(c.deliver_ttl_minutes,10) 依赖此列）。
-- 计费字段 billing_mode / bidding_price / cpa_event_prices 仍是广告主级（migration 000008/000010）。

ALTER TABLE ads_center.advertisers
  DROP COLUMN IF EXISTS end_at,
  DROP COLUMN IF EXISTS target_cpi,
  DROP COLUMN IF EXISTS actual_cpi,
  DROP COLUMN IF EXISTS consume_speed,
  DROP COLUMN IF EXISTS bidding_mode,
  DROP COLUMN IF EXISTS deliver_ttl_minutes;

ALTER TABLE ads_center.campaigns
  ADD COLUMN IF NOT EXISTS deliver_ttl_minutes integer NOT NULL DEFAULT 10;
