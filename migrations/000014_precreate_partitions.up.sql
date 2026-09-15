-- 分区预建：ad_events / decision_logs 跨月写入保障
--
-- 背景：两张表都是声明式分区表（PARTITION BY RANGE (ts)），000001 只预建了
-- 「当前月 + 未来 5 个月」，注释里就写着"自动维护任务后续补充，P0 手动建"。
-- 分区到期月份不存在时，写入会直接失败：
--   ERROR: no partition of relation "ad_events" found for row (SQLSTATE 23514/23P01)
--
-- 本迁移：一次性补建未来 12 个月（**幂等**，可重复执行，已存在的分区跳过）。
-- 长期维护：由 Go 服务的每日定时任务续建（store.EnsurePartitions），
-- 多实例并发由 pg_advisory_xact_lock 串行化，不会重复建、也不会互相报错。
DO $do$
DECLARE
    m   date := date_trunc('month', CURRENT_DATE)::date;
    i   int;
    tbl text;
BEGIN
    -- 并发串行化：多实例同时执行时，只有一个实例真正建表，其余等待后跳过
    PERFORM pg_advisory_xact_lock(hashtext('adcenter_ensure_partitions'));

    FOREACH tbl IN ARRAY ARRAY['ad_events', 'decision_logs'] LOOP
        FOR i IN 0..12 LOOP
            EXECUTE format(
                'CREATE TABLE IF NOT EXISTS ads_center.%I PARTITION OF ads_center.%I
                 FOR VALUES FROM (%L) TO (%L)',
                tbl || '_' || to_char(m + (i || ' month')::interval, 'YYYY_MM'),
                tbl,
                (m + (i || ' month')::interval)::date,
                (m + ((i + 1) || ' month')::interval)::date
            );
        END LOOP;
    END LOOP;
END
$do$;
