package config

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
)

// notifyChannel LISTEN/NOTIFY 通道（migration 000006 触发器写入端约定）。
const notifyChannel = "ads_center_config_changed"

const (
	// notifyMinInterval 两条通知驱动的重载之间的**最小间隔**（合并窗口）。
	//
	// 为什么必须加：只要出现"回调里写了一张挂 notify 触发器的表"的写法，就会形成
	// UPDATE → pg_notify → 回调 → 再 UPDATE 的自激回路。2026-10 事故即因此而起
	// （campaigns 写回触发 ~500 条 Redis 命令/秒 的循环，两天烧掉 $250）。
	// 节流**不消除**回路，但把它的破坏力封顶在每秒几次：既打不爆 Redis/DB，
	// 日志里也会立刻显形，不至于只能等账单。
	notifyMinInterval = 1 * time.Second
	// notifyDrainQuiet 排空判定的静默时长：连续这么久没有新通知就认为这一波攒完了。
	notifyDrainQuiet = 20 * time.Millisecond
)

// RunListener 订阅配置变更通知并即时重载（毫秒级生效）。
//
// 工程要点：
//   - LISTEN 必须独占一条连接（不入池，Postgres 按连接推送）
//   - 断线自动重连（外层 for），重连成功后立即做一次全量 Reload
//     补上断线窗口内可能漏掉的通知
//   - WaitForNotification 阻塞挂起，零轮询开销
//   - 通知**合并 + 节流**（见 notifyMinInterval）：一波通知先攒够一个窗口、
//     按表名去重后各重载一次，最坏速率封顶在「表种类数 / 秒」，杜绝上面的自激回路
//     把命令量放大成账单。单次变更的生效延迟只增加约 notifyDrainQuiet（20ms）
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
	if err := c.reload(ctx, "reconnect"); err != nil {
		log.Warn("config reload after reconnect failed", "err", err)
	}

	var lastReload time.Time

	for {
		n, err := conn.WaitForNotification(ctx)
		if err != nil {
			log.Warn("config listener lost connection", "err", err)
			return err
		}

		// ① 先睡满合并窗口（上次重载太近就等到间隔够）。睡眠期间到达的通知
		//    在连接里排队，不会丢——下一步会把它们一次性取走。
		if d := notifyMinInterval - time.Since(lastReload); d > 0 {
			time.Sleep(d)
		}

		// ② 排空本窗口内已在队列里的通知，按表名去重。
		//    必须排空而不是"每条各重载一次"：否则回路产生通知的速度（几十/秒）
		//    永远快过我们消费的速度，Postgres 侧给这个监听会话排的队列会无限堆积。
		pending := map[string]struct{}{n.Payload: {}}
		for {
			dctx, dcancel := context.WithTimeout(ctx, notifyDrainQuiet)
			n2, err2 := conn.WaitForNotification(dctx)
			dcancel()
			if err2 != nil {
				break // 静默 20ms 无新通知（或外部 ctx 结束）→ 这一波攒完
			}
			pending[n2.Payload] = struct{}{}
		}

		// ③ 按表各重载一次（去重后最多"表种类数"次），保持 OnChange 的路由语义：
		//    notify:campaigns → syncBudgets、notify:advertisers → syncWallets 等。
		lastReload = time.Now()
		for table := range pending {
			log.Info("config change notified", "table", table, "coalesced", len(pending))
			if err := c.reload(ctx, "notify:"+table); err != nil {
				// Reload 内部已保留旧快照并告警，此处继续处理下一张表
				continue
			}
		}
	}
}
