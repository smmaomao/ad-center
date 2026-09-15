-- 回滚 000032：恢复 tier 列、原复合索引，并移除 notes 列。
ALTER TABLE ads_center.advertisers DROP COLUMN notes;

DROP INDEX IF EXISTS ads_center.idx_advertisers_status;

ALTER TABLE ads_center.advertisers
    ADD COLUMN tier SMALLINT NOT NULL DEFAULT 2 CHECK (tier IN (1, 2, 3));

CREATE INDEX idx_advertisers_status_tier
    ON ads_center.advertisers (status, tier) WHERE deleted_at IS NULL;
