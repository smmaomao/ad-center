// Package redislog 提供 go-redis 命令级日志 Hook，用于排查首请求慢 / 命令耗时异常。
//
// 默认挂载即打印每条 Redis 命令（cmd + 首参 key + 耗时 + 错误）；设置环境变量
// REDIS_LOG=0 / false / off 可关闭，避免生产环境刷屏。排查完记得关掉。
package redislog

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
)

// LogHook 记录每次 Redis 命令的耗时与结果。
type LogHook struct {
	log *slog.Logger
}

// NewHook 构造命令日志 Hook；log 为 nil 时回退到 slog.Default()。
func NewHook(log *slog.Logger) *LogHook {
	if log == nil {
		log = slog.Default()
	}
	return &LogHook{log: log}
}

// Enabled 默认开启；REDIS_LOG 取 0 / false / off（大小写均可）时关闭。
func Enabled() bool {
	switch os.Getenv("REDIS_LOG") {
	case "0", "false", "off", "OFF":
		return false
	default:
		return true
	}
}

// Attach 若 REDIS_LOG 未关闭，给 client 挂上命令日志 Hook。
func Attach(client *redis.Client) {
	if Enabled() {
		client.AddHook(NewHook(nil))
	}
}

// DialHook 连接级钩子（此处透传，不记录）。
func (h *LogHook) DialHook(next redis.DialHook) redis.DialHook { return next }

// ProcessHook 单条命令：记录命令名 / 首参 / 耗时 / 错误。
func (h *LogHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		start := time.Now()
		err := next(ctx, cmd)
		h.log.Info("redis cmd",
			"cmd", cmd.Name(),
			"key", firstKey(cmd),
			"dur_ms", time.Since(start).Seconds()*1000,
			"err", errString(err),
		)
		return err
	}
}

// ProcessPipelineHook 管道：记录命令条数 / 耗时 / 错误。
func (h *LogHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		start := time.Now()
		err := next(ctx, cmds)
		h.log.Info("redis pipeline",
			"n", len(cmds),
			"dur_ms", time.Since(start).Seconds()*1000,
			"err", errString(err),
		)
		return err
	}
}

// firstKey 取命令的第二个参数（通常是 key）作为概览；脚本类命令此处是 sha / numkeys，仅供参考。
func firstKey(cmd redis.Cmder) string {
	args := cmd.Args()
	if len(args) < 2 {
		return ""
	}
	if s, ok := args[1].(string); ok {
		return s
	}
	return ""
}

// errString 把 redis.Nil（正常未命中）归一为 "nil"，避免刷屏完整 error。
func errString(err error) string {
	if err == nil {
		return ""
	}
	if err == redis.Nil {
		return "nil"
	}
	return err.Error()
}
