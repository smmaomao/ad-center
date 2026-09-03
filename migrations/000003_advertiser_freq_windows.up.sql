-- V1.2：广告主级多窗口滑动频控（PRD FR-02 投放配置 / 5.5 频控交互）
-- 例：3小时最多3次 + 24小时最多10次，多档同时生效；全滑动窗口，无自然日重置
ALTER TABLE ads_center.advertisers
    ADD COLUMN freq_windows JSONB NOT NULL
    DEFAULT '[{"window_minutes": 180, "max": 3}, {"window_minutes": 1440, "max": 10}]'::jsonb;

COMMENT ON COLUMN ads_center.advertisers.freq_windows IS
    '多窗口滑动频控：[{"window_minutes": 180, "max": 3}, {"window_minutes": 1440, "max": 10}]，多档同时生效，滑动窗口无自然日重置';
