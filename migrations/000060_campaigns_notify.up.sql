-- 补齐 ConfigCache 通知触发器：campaigns（广告任务）表建表于 000026，漏加了 notify 触发器。
-- 影响：后台上下架 / 改价 / 改排期广告任务时执行的都是 UPDATE campaigns，不会发
-- ads_center_config_changed，配置快照不能即时重载，只能等 60s 定时对账才生效
-- （表现为「刚上架广告任务，广告列表还是空的」）。
-- 补上后：campaigns 的 INSERT/UPDATE/DELETE 即时触发快照重载（并带动预算同步回调，
-- 因为预算单元是 campaign 级）。
CREATE TRIGGER trg_campaigns_notify
    AFTER INSERT OR UPDATE OR DELETE ON ads_center.campaigns
    FOR EACH ROW EXECUTE FUNCTION ads_center.notify_config_changed();
