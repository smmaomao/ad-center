// Package migrate 提供极简的数据库迁移 runner：
//   - 迁移文件来自构建期 go:embed 的 migrations/*.up.sql（见根包 adcenter.MigrationFS）
//   - 用 ads_center.schema_migrations 记录已执行版本，幂等、可重复运行
//   - 启动时对「已存在但无追踪记录的库（admin_users 已存在）」自动 baseline，
//     避免覆盖已有手工迁移而崩溃；对全新库则按版本号顺序执行全部迁移
//   - 多实例并发启动时用 pg_advisory_lock 串行化，避免同时建表冲突
package migrate

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	migrationRe = regexp.MustCompile(`^(\d+)_(.+)\.up\.sql$`)
	lockKey     = int64(987654321) // 迁移串行化用，任取一个固定值
)

type migration struct {
	version int
	name    string
	sql     string
}

// Up 在应用启动时调用：确保追踪表存在，按版本号顺序应用所有未执行的迁移。
// 对已存在但未接入追踪的库会自动 baseline（不重复执行 SQL）。
func Up(ctx context.Context, pool *pgxpool.Pool, fsys embed.FS, log *slog.Logger) error {
	return withLock(ctx, pool, func() error {
		if err := ensureTable(ctx, pool); err != nil {
			return fmt.Errorf("init tracking table: %w", err)
		}
		all, err := loadMigrations(fsys)
		if err != nil {
			return err
		}
		applied, err := queryApplied(ctx, pool)
		if err != nil {
			return fmt.Errorf("read applied: %w", err)
		}

		// 全新库无追踪记录，但核心表已存在 → 视为「手工迁移过的旧库」，自动 baseline。
		if len(applied) == 0 && coreTableExists(ctx, pool) {
			if err := baselineAll(ctx, pool, all); err != nil {
				return err
			}
			log.Warn("migrate: detected existing database with no migration history; " +
				"recorded all migrations as applied (baseline). " +
				"If this DB is NOT fully migrated, fix it manually before relying on it.")
			return nil
		}
		return applyPending(ctx, pool, all, applied, log)
	})
}

// Baseline 手动将版本号 <= to（to==nil 表示全部）的迁移标记为已执行，不执行其 SQL。
// 用于把「已有库」接入追踪系统：先确认这些版本在库里已真实存在，再 baseline。
func Baseline(ctx context.Context, pool *pgxpool.Pool, fsys embed.FS, log *slog.Logger, to *int) error {
	return withLock(ctx, pool, func() error {
		if err := ensureTable(ctx, pool); err != nil {
			return err
		}
		all, err := loadMigrations(fsys)
		if err != nil {
			return err
		}
		applied, err := queryApplied(ctx, pool)
		if err != nil {
			return err
		}
		tx, err := pool.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		n := 0
		for _, m := range all {
			if applied[m.version] {
				continue
			}
			if to != nil && m.version > *to {
				continue
			}
			if _, err := tx.Exec(ctx,
				`INSERT INTO ads_center.schema_migrations(version,name) VALUES($1,$2) ON CONFLICT DO NOTHING`,
				m.version, m.name); err != nil {
				return err
			}
			n++
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		log.Info("migrate: baseline recorded", "count", n)
		return nil
	})
}

// Status 打印每个迁移的 applied/pending 状态，便于排查。
func Status(ctx context.Context, pool *pgxpool.Pool, fsys embed.FS, log *slog.Logger) error {
	return withLock(ctx, pool, func() error {
		if err := ensureTable(ctx, pool); err != nil {
			return err
		}
		all, err := loadMigrations(fsys)
		if err != nil {
			return err
		}
		applied, err := queryApplied(ctx, pool)
		if err != nil {
			return err
		}
		for _, m := range all {
			state := "pending"
			if applied[m.version] {
				state = "applied"
			}
			log.Info("migrate", "version", m.version, "name", m.name, "state", state)
		}
		log.Info("migrate: summary", "total", len(all), "applied", len(applied))
		return nil
	})
}

// withLock 用会话级 advisory lock 串行化迁移，避免多实例并发启动时抢建同一张表。
func withLock(ctx context.Context, pool *pgxpool.Pool, fn func() error) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire conn: %w", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, lockKey); err != nil {
		return fmt.Errorf("acquire lock: %w", err)
	}
	defer conn.Exec(ctx, `SELECT pg_advisory_unlock($1)`, lockKey)
	return fn()
}

func ensureTable(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
		CREATE SCHEMA IF NOT EXISTS ads_center;
		CREATE TABLE IF NOT EXISTS ads_center.schema_migrations (
			version    bigint     PRIMARY KEY,
			name       text       NOT NULL,
			applied_at timestamptz NOT NULL DEFAULT now()
		);`)
	return err
}

func queryApplied(ctx context.Context, pool *pgxpool.Pool) (map[int]bool, error) {
	rows, err := pool.Query(ctx, `SELECT version FROM ads_center.schema_migrations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[int]bool{}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		m[v] = true
	}
	return m, rows.Err()
}

// coreTableExists 判断 ads_center.admin_users 是否已存在（核心表，早期迁移创建）。
// 用于区分「全新库」与「手工迁移过的旧库」。information_schema 在不含该 schema 时
// 也安全返回 0，不会报错。
func coreTableExists(ctx context.Context, pool *pgxpool.Pool) bool {
	const q = `SELECT count(*) FROM information_schema.tables
	           WHERE table_schema='ads_center' AND table_name='admin_users'`
	var n int
	if err := pool.QueryRow(ctx, q).Scan(&n); err != nil {
		return false
	}
	return n > 0
}

func loadMigrations(fsys embed.FS) ([]migration, error) {
	entries, err := fs.ReadDir(fsys, "migrations")
	if err != nil {
		return nil, fmt.Errorf("read migrations dir: %w", err)
	}
	var out []migration
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		m := migrationRe.FindStringSubmatch(e.Name())
		if m == nil {
			continue // 跳过非 .up.sql 文件
		}
		ver, err := strconv.Atoi(m[1])
		if err != nil {
			return nil, fmt.Errorf("bad version in %s: %w", e.Name(), err)
		}
		b, err := fs.ReadFile(fsys, "migrations/"+e.Name())
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", e.Name(), err)
		}
		out = append(out, migration{
			version: ver,
			name:    strings.TrimSuffix(m[2], ".up"),
			sql:     string(b),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}

func applyPending(ctx context.Context, pool *pgxpool.Pool, all []migration, applied map[int]bool, log *slog.Logger) error {
	n := 0
	for _, m := range all {
		if applied[m.version] {
			continue
		}
		if err := runOne(ctx, pool, m); err != nil {
			return fmt.Errorf("apply %s (v%d): %w", m.name, m.version, err)
		}
		log.Info("migrate: applied", "version", m.version, "name", m.name)
		n++
	}
	if n == 0 {
		log.Info("migrate: database up to date")
	} else {
		log.Info("migrate: applied migrations", "count", n)
	}
	return nil
}

// runOne 在单个事务内执行迁移 SQL 并写入追踪记录，保证原子性（无 CONCURRENTLY）。
func runOne(ctx context.Context, pool *pgxpool.Pool, m migration) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, m.sql); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO ads_center.schema_migrations(version,name) VALUES($1,$2)`,
		m.version, m.name); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func baselineAll(ctx context.Context, pool *pgxpool.Pool, all []migration) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for _, m := range all {
		if _, err := tx.Exec(ctx,
			`INSERT INTO ads_center.schema_migrations(version,name) VALUES($1,$2) ON CONFLICT DO NOTHING`,
			m.version, m.name); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
