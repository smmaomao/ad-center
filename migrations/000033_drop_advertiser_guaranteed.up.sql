-- 彻底移除广告主级保量份额（用户决策：保量概念下放到 campaign / slot 级，
-- 广告主不再持有 guaranteed_enabled / guaranteed_min_share）。
-- 注意：campaigns 表的保量字段（migration 000031）与 fill_priorities 的
-- guaranteed_share 不受影响，继续保留。
ALTER TABLE ads_center.advertisers
    DROP COLUMN IF EXISTS guaranteed_enabled,
    DROP COLUMN IF EXISTS guaranteed_min_share;
