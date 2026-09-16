// migrate 命令行工具：手动控制数据库迁移（不参与生产镜像，本地使用）。
//
// 子命令：
//
//	up                应用所有未执行的迁移（等价于服务启动时自动执行的逻辑）
//	baseline [--to N] 将版本 <= N（默认全部）标记为已执行，不跑 SQL —— 用于把已有库接入追踪
//	status            列出每个迁移的 applied/pending
//	create <name>     生成下一版本的 .up.sql / .down.sql 骨架
//
// 连接串取环境变量 DATABASE_URL。
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"adcenter"
	"adcenter/internal/migrate"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Error("DATABASE_URL not set")
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		log.Error("connect db", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	switch cmd {
	case "up":
		must(migrate.Up(ctx, pool, adcenter.MigrationFS, log), log)
	case "baseline":
		fs := flag.NewFlagSet("baseline", flag.ExitOnError)
		to := fs.Int("to", 0, "仅 baseline 版本 <= to（0 = 全部）")
		_ = fs.Parse(args)
		var toPtr *int
		if *to > 0 {
			toPtr = to
		}
		must(migrate.Baseline(ctx, pool, adcenter.MigrationFS, log, toPtr), log)
	case "status":
		must(migrate.Status(ctx, pool, adcenter.MigrationFS, log), log)
	case "create":
		if len(args) < 1 {
			log.Error("usage: migrate create <name>")
			os.Exit(2)
		}
		must(createMigration(args[0]), log)
	default:
		usage()
		os.Exit(2)
	}
}

func must(err error, log *slog.Logger) {
	if err != nil {
		log.Error("migration failed", "err", err)
		os.Exit(1)
	}
}

// createMigration 计算下一个版本号并生成 up/down 骨架。
func createMigration(name string) error {
	entries, err := os.ReadDir("migrations")
	if err != nil {
		return err
	}
	maxVer := 0
	for _, e := range entries {
		var v int
		if _, err := fmt.Sscanf(e.Name(), "%05d_", &v); err == nil && v > maxVer {
			maxVer = v
		}
	}
	ver := maxVer + 1
	fn := fmt.Sprintf("%05d_%s", ver, strings.TrimSpace(name))
	up := filepath.Join("migrations", fn+".up.sql")
	down := filepath.Join("migrations", fn+".down.sql")

	header := fmt.Sprintf("-- %s (v%d)\n-- 在此编写上线迁移。建议用 IF NOT EXISTS / ADD COLUMN IF NOT EXISTS 保持幂等。\n\n", fn, ver)
	if err := os.WriteFile(up, []byte(header), 0o644); err != nil {
		return err
	}
	dheader := fmt.Sprintf("-- %s 回滚\n-- 在此编写与 up 对应的回滚（DROP / ALTER ... DROP COLUMN 等）。\n\n", fn)
	if err := os.WriteFile(down, []byte(dheader), 0o644); err != nil {
		return err
	}
	fmt.Printf("created %s\ncreated %s\n", up, down)
	return nil
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage: migrate <up|baseline|status|create> [args]

  up                应用所有未执行的迁移
  baseline [--to N] 将版本 <= N 标记为已执行（不跑 SQL）
  status            列出每个迁移的 applied/pending
  create <name>     生成下一版本迁移骨架

连接串取环境变量 DATABASE_URL。`)
}
