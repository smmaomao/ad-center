-- V1.2：投放截止时间 + 素材多规格（横竖屏）+ 素材形态扩展（网页）

-- 1. 广告主投放截止时间（NULL = 永续投放；到期由决策引擎过滤，功能阶段 1 实现）
ALTER TABLE ads_center.advertisers
    ADD COLUMN end_at TIMESTAMPTZ;

COMMENT ON COLUMN ads_center.advertisers.end_at IS '投放截止时间，NULL=永续；过期广告主不参与决策';

-- 2. 素材方向（横屏/竖屏/方形）——短剧 App 以竖屏为主，客户端按位置自选
--    数据库里历史行默认竖屏（无方向限制语义时用 any）
ALTER TABLE ads_center.creatives
    ADD COLUMN orientation TEXT NOT NULL DEFAULT 'any'
    CHECK (orientation IN ('landscape', 'portrait', 'square', 'any'));

-- 3. 素材形态扩展：视频 / 图片 / 网页（H5 落地页，storage_path 存 URL）
--    原 CHECK(video|image) 替换为含 html 的新约束
ALTER TABLE ads_center.creatives
    DROP CONSTRAINT creatives_media_type_check;
ALTER TABLE ads_center.creatives
    ADD CONSTRAINT creatives_media_type_check
    CHECK (media_type IN ('video', 'image', 'html'));

COMMENT ON COLUMN ads_center.creatives.media_type IS '素材形态：video=视频 / image=图片 / html=网页(H5)，storage_path 存对象路径或 URL';

-- 方向检索索引
CREATE INDEX idx_creatives_orientation ON ads_center.creatives (advertiser_id, status, orientation);
