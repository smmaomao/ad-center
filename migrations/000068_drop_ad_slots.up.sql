-- #10 清理：ad_slots / fill_priorities 已确认废弃（无生产调用方，频控实际由
-- campaigns 表承载，engine 用 camp.Freq* 构造 AdvPolicy）。两张表仅是遗留死配置，
-- 删除避免干扰。
--
-- 顺序：先删子表 fill_priorities（其 slot_code/slot_id 外键指向 ad_slots），再删父表
-- ad_slots。ad_slots 用 CASCADE：历史上 creatives / ad_events / metrics_minute /
-- decision_logs 等表曾以 slot_id 外键指向 ad_slots，部分约束已随后续迁移移除，但为稳妥
-- 用 CASCADE 一并清理所有残留依赖（仅删除外键约束与触发器，不会删除引用表本身）。
DROP TABLE IF EXISTS ads_center.fill_priorities;
DROP TABLE IF EXISTS ads_center.ad_slots CASCADE;
