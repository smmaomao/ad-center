-- 回滚 000033：恢复广告主级保量字段（沿用 000001 的原定义）。
ALTER TABLE ads_center.advertisers
    ADD COLUMN guaranteed_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN guaranteed_min_share NUMERIC(5, 4) NOT NULL DEFAULT 0
        CHECK (guaranteed_min_share BETWEEN 0 AND 1);
