package store

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
)

// adminAuthChannel RBAC 缓存失效广播通道（migration 000069 触发器为写端，
// payload 是表名）。
const adminAuthChannel = "ads_center_admin_auth_changed"

// RunAdminAuthListener 订阅 RBAC 变更通知，清空本进程的角色 / 菜单缓存。
//
// 为什么需要它：缓存是进程内的，多实例部署时处理写请求的实例能立刻失效
// 自己的缓存，其余实例不知道 —— 降权 / 删号后那些实例会继续按旧角色放行，
// 最长残留一个 TTL。有了广播，失效窗口从 TTL（分钟级）缩到毫秒级，TTL
// 退化为"监听断线"时的兜底。
//
// 工程要点（与 config 的 LISTEN 一致）：
//   - LISTEN 必须独占一条连接（不入池，Postgres 按连接推送）
//   - 断线自动重连（外层 for + 3s 退避），重连成功后先清一次缓存，
//     补上断线窗口内可能漏掉的通知
//   - WaitForNotification 阻塞挂起，零轮询开销
func (s *Store) RunAdminAuthListener(ctx context.Context, dsn string, log *slog.Logger) {
	for {
		if ctx.Err() != nil {
			return
		}
		_ = s.adminAuthListenLoop(ctx, dsn, log)
		if ctx.Err() != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}
	}
}

func (s *Store) adminAuthListenLoop(ctx context.Context, dsn string, log *slog.Logger) error {
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		log.Warn("admin auth listener connect failed", "err", err)
		return err
	}
	defer conn.Close(context.Background())

	if _, err := conn.Exec(ctx, "LISTEN "+adminAuthChannel); err != nil {
		log.Warn("admin auth listener LISTEN failed", "err", err)
		return err
	}
	log.Info("admin auth listener ready", "channel", adminAuthChannel)

	// 重连成功后先清一次：断线窗口内可能漏掉通知
	s.InvalidateAdminAuth()

	for {
		n, err := conn.WaitForNotification(ctx)
		if err != nil {
			log.Warn("admin auth listener lost connection", "err", err)
			return err
		}
		s.InvalidateAdminAuth()
		log.Debug("admin auth cache invalidated", "table", n.Payload)
	}
}
