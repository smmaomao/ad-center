-- 回滚：移除 banner。若已存在 type='banner' 的广告位，回滚会因 CHECK 失败，
-- 需先清理相关行。
ALTER TABLE ad_slots DROP CONSTRAINT ad_slots_type_check;
ALTER TABLE ad_slots ADD CONSTRAINT ad_slots_type_check
    CHECK (type IN ('rewarded_video', 'splash', 'interstitial', 'feed'));
