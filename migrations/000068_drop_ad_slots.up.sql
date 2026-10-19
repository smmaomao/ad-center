-- #10 清理：ad_slots / fill_priorities 已确认废弃（无生产调用方，频控实际由
-- campaigns 表承载，engine 用 camp.Freq* 构造 AdvPolicy）。两张表仅是遗留死配置，
-- 删除避免干扰。fill_priorities 引用 ad_slots(slot_code)，先删子表。
DROP TABLE IF EXISTS fill_priorities;
DROP TABLE IF EXISTS ad_slots;
