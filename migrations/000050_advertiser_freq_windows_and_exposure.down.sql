-- 回滚 000050（仅恢复列结构与派生值，不恢复精确历史枚举）
ALTER TABLE ads_center.campaigns
    ALTER COLUMN consume_speed TYPE text
    USING (
      CASE
        WHEN consume_speed >= 9 THEN 'asap'
        WHEN consume_speed >= 7 THEN 'accelerated'
        ELSE 'even'
      END
    ),
    ALTER COLUMN consume_speed SET DEFAULT 'even';

ALTER TABLE ads_center.advertisers
    ADD COLUMN IF NOT EXISTS freq_windows jsonb NOT NULL DEFAULT '[]'::jsonb;
