-- 彻底移除广告主 Tier 概念（用户决策：广告主与产品均不再使用 Tier 分层）。
ALTER TABLE ads_center.advertisers DROP COLUMN tier;

-- 原复合索引 (status, tier) 因 tier 列删除失效，重建为仅 status 索引。
DROP INDEX IF EXISTS ads_center.idx_advertisers_status_tier;
CREATE INDEX idx_advertisers_status
    ON ads_center.advertisers (status) WHERE deleted_at IS NULL;

-- 广告主管理页新增「备注」字段。
ALTER TABLE ads_center.advertisers ADD COLUMN notes TEXT NOT NULL DEFAULT '';
