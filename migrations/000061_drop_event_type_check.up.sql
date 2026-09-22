-- 事件类型不在表级做校验：事件名由代码侧统一约束
-- （config.ConversionEvents 与各事件产出点），表级 CHECK 只会带来
-- "新增一个事件类型就必须改迁移"的负担，且约束名分散难维护。
-- 这里移除 ad_conversions 与 ad_events 的 event_type CHECK。
ALTER TABLE ads_center.ad_conversions
    DROP CONSTRAINT IF EXISTS ad_conversions_event_type_check;

ALTER TABLE ads_center.ad_events
    DROP CONSTRAINT IF EXISTS ad_events_event_type_check;
