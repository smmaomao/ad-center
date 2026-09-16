-- 第 5 种展现样式 banner（横幅）：
-- 000001 里 ad_slots.type 的 CHECK 只允许 4 种，这里放宽加入 banner。
-- ad_slots / fill_priorities 表保留（后台广告位管理仍可用），运行时决策不读取。
ALTER TABLE ad_slots DROP CONSTRAINT ad_slots_type_check;
ALTER TABLE ad_slots ADD CONSTRAINT ad_slots_type_check
    CHECK (type IN ('rewarded_video', 'splash', 'interstitial', 'feed', 'banner'));
