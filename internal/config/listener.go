package config

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
)

// notifyChannel LISTEN/NOTIFY 通道（migration 000006 触发器写入端约定）。
const notifyChannel = "ads_center_config_changed"

// RunListener 订阅配置变更通知并即时重载（毫秒级生效）。
//
// 工程要点：
//   - LISTEN 必须独占一条连接（不入池，Postgres 按连接推送）
//   - 断线自动重连（外层 for），重连成功后立即做一次全量 Reload
//     补上断线窗口内可能漏掉的通知
//   - WaitForNotification 阻塞挂起，零轮询开销
func (c *Cache) RunListener(ctx context.Context, dsn string, log *slog.Logger) {
	for {
		if ctx.Err() != nil {
			return
		}
		_ = c.listenLoop(ctx, dsn, log)
		if ctx.Err() != nil {
			return
		}
		// 连接异常退出：退避重连
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}
	}
}

func (c *Cache) listenLoop(ctx context.Context, dsn string, log *slog.Logger) error {
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		log.Warn("config listener connect failed", "err", err)
		return err
	}
	defer conn.Close(context.Background())

	if _, err := conn.Exec(ctx, "LISTEN "+notifyChannel); err != nil {
		log.Warn("config listener LISTEN failed", "err", err)
		return err
	}
	log.Info("config listener ready", "channel", notifyChannel)

	// 重连成功后先补一次全量（防断线窗口丢通知）
	if err := c.Reload(ctx); err != nil {
		log.Warn("config reload after reconnect failed", "err", err)
	}

	for {
		n, err := conn.WaitForNotification(ctx)
		if err != nil {
			log.Warn("config listener lost connection", "err", err)
			return err
		}
		log.Info("config change notified", "table", n.Payload)
		if err := c.Reload(ctx); err != nil {
			// Reload 内部已保留旧快照并告警，此处继续等下一条通知
			continue
		}
	}
}
