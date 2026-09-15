// Package queue 事件队列抽象（M2，SCALING.md §2）。
//
// 目标：把事件的落库从「请求路径同步写」挪到「队列 + 消费端批量写」，
// 由消费端控制写入速率（削峰填谷），并让多实例下事件不丢、可分摊消费。
//
// 两种后端，接口一致：
//   - Memory：进程内 channel + 攒批落库（现状，单实例足够，进程重启丢在途）
//   - Redis ：Redis Streams + consumer group（多实例分摊、ACK/重试、积压可见）
//
// 启动开关 QUEUE_DRIVER=memory|redis。
package queue

import (
	"context"
	"sync/atomic"
	"time"

	"adcenter/internal/store"
)

// Handler 批量消费回调。
//
// 返回 error 表示处理失败：Redis 后端不 ACK（消息留在 PEL 等下次重试）；
// Memory 后端无重试能力，仅计入 dropped（与现状一致，由对账修正）。
type Handler func(ctx context.Context, batch []store.AdEvent) error

// Stats 队列运行指标（M3 监控面板数据源）。
type Stats struct {
	Buffered int64 // 队列积压：Redis=XLEN，Memory=channel 长度
	Pending  int64 // 已投递未确认（仅 Redis 有意义；持续增长=消费卡住）
	Dropped  int64 // 入队失败或处理失败的丢弃数
}

// Backend 事件队列。实现必须并发安全。
type Backend interface {
	// Publish 入队。必须非阻塞——决策/回执路径绝不因队列变慢。
	Publish(e store.AdEvent)

	// Run 启动消费循环（后台协程），ctx 取消时尽力排空后退出。
	Run(ctx context.Context, h Handler)

	// Stats 队列指标（低频调用：/healthz、管理 API）。
	Stats() Stats

	// Close 释放资源。
	Close() error
}

// ============ Memory 后端（现状行为，单实例） ============

// Memory 进程内 channel 队列：入队非阻塞（满则丢弃并计数），
// 后台协程按 batchSize / interval 攒批交给 Handler。
type Memory struct {
	ch        chan store.AdEvent
	dropped   atomic.Int64
	failed    atomic.Int64
	batchSize int
	interval  time.Duration
}

// NewMemory 创建进程内队列。size=缓冲容量，batchSize=落库批量，interval=攒批超时。
func NewMemory(size, batchSize int, interval time.Duration) *Memory {
	return &Memory{
		ch:        make(chan store.AdEvent, size),
		batchSize: batchSize,
		interval:  interval,
	}
}

func (m *Memory) Publish(e store.AdEvent) {
	select {
	case m.ch <- e:
	default:
		m.dropped.Add(1) // 队列满：丢弃并计数（决策路径绝不阻塞）
	}
}

func (m *Memory) Run(ctx context.Context, h Handler) {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	batch := make([]store.AdEvent, 0, m.batchSize)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := h(context.Background(), batch); err != nil {
			m.failed.Add(1)
		}
		batch = batch[:0]
	}
	for {
		select {
		case <-ctx.Done():
			// 退出前尽力排空
			for {
				select {
				case e := <-m.ch:
					batch = append(batch, e)
				default:
					flush()
					return
				}
			}
		case e := <-m.ch:
			batch = append(batch, e)
			if len(batch) >= m.batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

func (m *Memory) Stats() Stats {
	return Stats{Buffered: int64(len(m.ch)), Dropped: m.dropped.Load() + m.failed.Load()}
}

func (m *Memory) Close() error { return nil }

var _ Backend = (*Memory)(nil)
