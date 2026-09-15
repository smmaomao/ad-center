ALTER TABLE ads_center.campaigns
    DROP COLUMN IF EXISTS actual_cpi,
    DROP COLUMN IF EXISTS spent_today,
    DROP COLUMN IF EXISTS consume_speed,
    DROP COLUMN IF EXISTS guaranteed_enabled,
    DROP COLUMN IF EXISTS guaranteed_min_share,
    DROP COLUMN IF EXISTS priority_score;
