package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"adcenter/internal/store"

	"github.com/redis/go-redis/v9"

	"adcenter/internal/redislog"
)

// Redis 基于 Redis Streams 的事件队列（M2，SCALING.md §2/§3）。
//
// 为什么是 Streams 而不是 Pub/Sub：Pub/Sub 不持久化、无 ACK、消费者掉线即丢消息。
// Streams 有 consumer group、PEL（未确认列表）、MAXLEN 裁剪，天然满足：
//   - 削峰：生产端一次 XADD（亚毫秒），峰值堆在队列里，消费端按 DB 能力匀速消费
//   - 不丢：消息持久化 + ACK，进程重启/崩溃后由 PEL 重投
//   - 分摊：多实例以同一 group 不同 consumer 消费，天然负载均衡
//
// 投递语义：至少一次。ad_events 可能重复（M3 补幂等键），
// budget_ledger 靠 (广告主, 小时) 的 ON CONFLICT 累加保证金额不重复计算。
//
// 消费端 XREADGROUP BLOCK：TCP 长阻塞，空闲时每 defaultBlock 才一次往返。
// 注意 go-redis 会把带 BLOCK 的 XREADGROUP 的读超时自动放宽为 block+10s
// （stream_commands.go: cmd.setReadTimeout(a.Block)），所以客户端默认的
// 5s ReadTimeout 不会把阻塞读打断——Upstash 也要求"客户端超时 > 命令超时"。
const defaultBlock = 30 * time.Second

type Redis struct {
	client    *redis.Client
	stream    string
	group     string
	consumer  string
	maxLen    int64 // XADD MAXLEN（~ 近似裁剪）
	batchSize int
	block     time.Duration

	mu      sync.Mutex
	buf     []store.AdEvent // 生产端攒批
	dropped atomic.Int64    // 入队失败
	failed  atomic.Int64    // 消费处理失败
}

// NewRedis 构造 Streams 队列（必要时创建 group）。
//
// 连接必须是**原生 Redis 协议**的 TCP/TLS 端点（redis:// / rediss://），不能用
// Upstash 的 REST 端点：REST 是 HTTP 请求式，不支持阻塞版 XREAD/XREADGROUP，
// 只能定时轮询（空闲也每秒发请求，既贵又有延迟）；TCP 下 BLOCK 期间请求挂在
// 服务端，没消息就不产生请求。见 SCALING.md §3。
func NewRedis(redisURL, prefix, group string, batchSize int) (*Redis, error) {
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}
	// 空闲时的阻塞时长（可用 QUEUE_BLOCK 覆盖，如 "15s"）。太长会让关停时
	// 多等一轮；太短则空转往返变多（Upstash 按请求计费）。
	block := defaultBlock
	if v := os.Getenv("QUEUE_BLOCK"); v != "" {
		if d, perr := time.ParseDuration(v); perr == nil && d > 0 {
			block = d
		}
	}
	host, _ := os.Hostname()
	client := redis.NewClient(opt)
	redislog.Attach(client)
	r := &Redis{
		client:    client,
		stream:    prefix + "events",
		group:     group,
		consumer:  host + "-" + strconv.Itoa(os.Getpid()),
		maxLen:    200000, // ≈ 数小时的事件量，足够吸收峰值
		batchSize: batchSize,
		block:     block,
		buf:       make([]store.AdEvent, 0, batchSize),
	}
	// 幂等建组：已存在返回 BUSYGROUP，忽略
	if err := r.client.XGroupCreateMkStream(context.Background(), r.stream, group, "0").Err(); err != nil &&
		!errors.Is(err, redis.Nil) && !strings.Contains(err.Error(), "BUSYGROUP") {
		return nil, err
	}
	return r, nil
}

// Publish 入队：先攒批，满批立即刷；低峰由 Run 的定时 flush 兜底。
func (r *Redis) Publish(e store.AdEvent) {
	r.mu.Lock()
	r.buf = append(r.buf, e)
	n := len(r.buf)
	r.mu.Unlock()
	if n >= r.batchSize {
		_ = r.flush(context.Background())
	}
}

// flush 把攒批一次性 XADD（单 pipeline，摊薄 RTT 与 Upstash 请求计费）。
func (r *Redis) flush(ctx context.Context) error {
	r.mu.Lock()
	if len(r.buf) == 0 {
		r.mu.Unlock()
		return nil
	}
	batch := r.buf
	r.buf = make([]store.AdEvent, 0, r.batchSize)
	r.mu.Unlock()

	pipe := r.client.Pipeline()
	for _, e := range batch {
		b, err := json.Marshal(e)
		if err != nil {
			continue
		}
		pipe.XAdd(ctx, &redis.XAddArgs{
			Stream: r.stream, MaxLen: r.maxLen, Approx: true,
			Values: map[string]any{"d": string(b)},
		})
	}
	if _, err := pipe.Exec(ctx); err != nil {
		r.dropped.Add(int64(len(batch)))
		return err
	}
	return nil
}

// Run 消费循环：XREADGROUP（阻塞） → 批量交给 Handler → 成功后 ACK。
func (r *Redis) Run(ctx context.Context, h Handler) {
	// 低峰兜底：定时把没攒满的批次刷出去
	go func() {
		t := time.NewTicker(10 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				_ = r.flush(context.Background())
				return
			case <-t.C:
				_ = r.flush(ctx)
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		res, err := r.client.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    r.group,
			Consumer: r.consumer,
			Streams:  []string{r.stream, ">"},
			Count:    int64(r.batchSize),
			Block:    r.block,
		}).Result()
		if err != nil {
			if ctx.Err() != nil {
				return // 退出中
			}
			if errors.Is(err, redis.Nil) {
				// BLOCK 到期仍无消息——真阻塞下的正常空转（每 block 一次，不刷命令数）。
				// 保留极小退避，仅为防止将来换成不真正阻塞的端点（如 REST）时退化为紧循环。
				select {
				case <-time.After(50 * time.Millisecond):
				case <-ctx.Done():
				}
				continue
			}
			// 连接类错误：短暂退避后重试，避免打满日志
			select {
			case <-time.After(200 * time.Millisecond):
			case <-ctx.Done():
				return
			}
			continue
		}

		for _, s := range res {
			batch := make([]store.AdEvent, 0, len(s.Messages))
			ids := make([]string, 0, len(s.Messages))
			for _, m := range s.Messages {
				if raw, ok := m.Values["d"].(string); ok {
					var e store.AdEvent
					if err := json.Unmarshal([]byte(raw), &e); err == nil {
						batch = append(batch, e)
					}
				}
				ids = append(ids, m.ID)
			}
			if len(batch) > 0 {
				if err := h(ctx, batch); err != nil {
					r.failed.Add(1)
					continue // 不 ACK：留在 PEL，稍后重投（配合聚合写幂等）
				}
			}
			if len(ids) > 0 {
				_ = r.client.XAck(ctx, r.stream, r.group, ids...).Err()
			}
		}
	}
}

// Stats 队列指标。Buffered=XLEN（含已 ACK 未裁剪的），Pending=PEL 未确认数。
// 监控应看 Pending（消费是否卡住）而非 Buffered。
func (r *Redis) Stats() Stats {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	var st Stats
	if n, err := r.client.XLen(ctx, r.stream).Result(); err == nil {
		st.Buffered = n
	}
	if p, err := r.client.XPending(ctx, r.stream, r.group).Result(); err == nil && p != nil {
		st.Pending = p.Count
	}
	st.Dropped = r.dropped.Load() + r.failed.Load()
	return st
}

func (r *Redis) Close() error {
	_ = r.flush(context.Background())
	return r.client.Close()
}

// Ping 探活（保活 goroutine 用），确保首个真实请求复用到热连接。
func (r *Redis) Ping(ctx context.Context) error { return r.client.Ping(ctx).Err() }

// Name 队列标识（日志/排障用）。
func (r *Redis) Name() string {
	return fmt.Sprintf("stream=%s group=%s consumer=%s", r.stream, r.group, r.consumer)
}

var _ Backend = (*Redis)(nil)
