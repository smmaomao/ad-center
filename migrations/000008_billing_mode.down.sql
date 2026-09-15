-- 回滚：恢复事件类型 CHECK、移除计费列
ALTER TABLE ads_center.ad_events
    DROP CONSTRAINT ad_events_event_type_check,
    ADD CONSTRAINT ad_events_event_type_check CHECK (event_type IN
        ('request', 'fill', 'impression', 'click', 'conversion'));

ALTER TABLE ads_center.advertisers
    DROP COLUMN billing_mode,
    DROP COLUMN cpa_event_prices;
