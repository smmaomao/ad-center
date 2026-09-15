package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
)

// partitionTables 按月预建分区的表（000001 起为声明式分区：PARTITION BY RANGE (ts)）。
var partitionTables = []string{"ad_events", "decision_logs"}

// ensurePartitionTmpl 预建当前月起 N 个月分区（含当前月）。
// %d = 月数-1、两个 %s = 表名（代码内常量，非外部输入，无注入风险）。
const ensurePartitionTmpl = `DO $do$
DECLARE
    m date := date_trunc('month', CURRENT_DATE)::date;
    i int;
BEGIN
    -- 并发串行化：多实例同时执行时只有一个在真正建表，其余等待后跳过
    PERFORM pg_advisory_xact_lock(hashtext('adcenter_ensure_partitions'));
    FOR i IN 0..%d LOOP
        EXECUTE format(
            'CREATE TABLE IF NOT EXISTS ads_center.%%I PARTITION OF ads_center.%%I
             FOR VALUES FROM (%%L) TO (%%L)',
            '%s_' || to_char(m + (i || ' month')::interval, 'YYYY_MM'),
            '%s',
            (m + (i || ' month')::interval)::date,
            (m + ((i + 1) || ' month')::interval)::date
        );
    END LOOP;
END
$do$;`

// EnsurePartitions 确保两张分区表未来 months 个月（含当前月）的分区存在。
//
// 为什么需要：ad_events / decision_logs 按月分区，目标月份分区不存在时写入会
// 直接失败（23P01 no partition of relation found for row）——表现是事件丢失、
// 接口 500，而且只在跨月那一刻才暴露，属于典型的"定时炸弹"。
//
// 多实例并发安全（同一个定时任务会在每台实例上各跑一次）：
//  1. `CREATE TABLE IF NOT EXISTS ... PARTITION OF`：已存在则跳过
//  2. `pg_advisory_xact_lock`：同一时刻只有一个实例在建分区。建分区需要对父表
//     加 ACCESS EXCLUSIVE 锁，串行化后 IF NOT EXISTS 的判断才可靠
//  3. 兜底忽略 duplicate_table（42P07），覆盖极端竞态
//
// 幂等：可每天重复执行。
func (s *Store) EnsurePartitions(ctx context.Context, months int) error {
	if months < 1 {
		months = 1
	}
	for _, tbl := range partitionTables {
		q := fmt.Sprintf(ensurePartitionTmpl, months-1, tbl, tbl)
		if _, err := s.pool.Exec(ctx, q); err != nil {
			if isDuplicateRelation(err) {
				continue
			}
			return fmt.Errorf("ensure partitions for %s: %w", tbl, err)
		}
	}
	return nil
}

// isDuplicateRelation 判断是否是"对象已存在"错误（并发建分区时可安全忽略）。
func isDuplicateRelation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "42P07" // duplicate_table
	}
	return false
}

// dropOldPartitionTmpl 删除某分区表、保留窗口之外的旧子分区。
//
// retainMonths = 保留最近多少个月（含当前月）。只处理命名符合 `^<tbl>_[0-9]{4}_[0-9]{2}$`
// 的真实子分区；整月都早于保留窗口起点的才 DROP。父表 / 非分区表 / 保留窗口内的
// 分区都不会被选中。%d = retainMonths，%s = 表名（均为代码内常量，无注入风险）。
const dropOldPartitionTmpl = `DO $do$
DECLARE
    keep_from date := (date_trunc('month', CURRENT_DATE) - (%d - 1 || ' month')::interval)::date;
    child     text;
    pname     text := '%s';
BEGIN
    -- 多实例串行化：DROP 分区需对父表 ACCESS EXCLUSIVE 锁，串行后 IF EXISTS 才可靠
    PERFORM pg_advisory_xact_lock(hashtext('adcenter_drop_partitions'));
    FOR child IN
        SELECT c.relname::text
        FROM pg_inherits i
        JOIN pg_class c ON c.oid = i.inhrelid
        JOIN pg_class p ON p.oid = i.inhparent
        WHERE p.relname = pname
          AND c.relname ~ ('^' || pname || '_[0-9]{4}_[0-9]{2}$')
    LOOP
        -- 分区上界 = 月份起点 + 1 月；上界 <= keep_from 表示整月都早于保留窗口
        IF to_date(right(child, 7), 'YYYY_MM') + interval '1 month' <= keep_from THEN
            EXECUTE format('DROP TABLE IF EXISTS ads_center.%%I', child);
        END IF;
    END LOOP;
END
$do$;`

// DropOldPartitions 删除 partitionTables 中每张表、保留窗口之外的旧子分区。
//
// retainMonths 表示"保留最近多少个月（含当前月）"。例如 retainMonths=13 时，
// 最旧保留月 = 当前月 - 12 个月，更早的子分区（整月都落在保留窗口之前）会被 DROP。
//
// 安全性（避免误删）：
//  1. 只扫描 pg_inherits 的真实子分区，且名称必须匹配 `^<tbl>_[0-9]{4}_[0-9]{2}$`
//     —— 父表本身不会被选中，名称不合规的分区也不会被碰。
//  2. 只删"整月都早于保留窗口"的分区（上界 <= keep_from），保留窗口内的分区不动。
//  3. DROP TABLE IF EXISTS 幂等；多实例并发由 pg_advisory_xact_lock 串行化。
//  4. dev 库当前最旧分区即当前月，默认保留 13 个月时实际不会删任何分区，
//     可放心先上线，待数据变老再按需收紧保留期。
//
// 幂等：可每天重复执行。
func (s *Store) DropOldPartitions(ctx context.Context, retainMonths int) error {
	if retainMonths < 1 {
		retainMonths = 1
	}
	for _, tbl := range partitionTables {
		q := fmt.Sprintf(dropOldPartitionTmpl, retainMonths, tbl)
		if _, err := s.pool.Exec(ctx, q); err != nil {
			return fmt.Errorf("drop old partitions for %s: %w", tbl, err)
		}
	}
	return nil
}
