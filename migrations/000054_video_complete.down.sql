-- 回滚 000054_video_complete：移除 video_complete 事件类型与 callback_ok 列。
ALTER TABLE ads_center.ad_events
    DROP CONSTRAINT IF EXISTS ad_events_event_type_check,
    ADD CONSTRAINT ad_events_event_type_check CHECK (event_type IN
        ('request', 'fill', 'impression', 'click', 'conversion',
         'install', 'activate', 'register', 'first_purchase', 'purchase'));

ALTER TABLE ad_events DROP COLUMN IF EXISTS callback_ok;
