-- 回滚：恢复事件类型 CHECK（ad_conversions 用 000056 版本、ad_events 用 000054 版本）。
-- 注意：若库中已存在不在列表内的事件类型（如 subscribe），恢复约束会失败，需先清理。
ALTER TABLE ads_center.ad_conversions
    ADD CONSTRAINT ad_conversions_event_type_check CHECK (event_type IN
        ('install', 'activate', 'register', 'first_purchase', 'purchase'));

ALTER TABLE ads_center.ad_events
    ADD CONSTRAINT ad_events_event_type_check CHECK (event_type IN
        ('request', 'fill', 'impression', 'click', 'conversion',
         'install', 'activate', 'register', 'first_purchase', 'purchase',
         'video_complete'));
