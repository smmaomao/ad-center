package fatigue

import (
	"context"
	"time"

	"adcenter/internal/config"

	"github.com/redis/go-redis/v9"
)

// RedisStore 疲劳度控制的 Redis 实现（多实例共享，与 budget/frequency 同一 REDIS_URL）。
// 错误一律 fail-open（放行），与频控层降级策略一致——Redis 不可用时不挡广告，
// 事后由对账/限额（预算）兜底，避免"想限流反而把正常流量全卡死"。
//
// key 隔离：本项目用独立的 Redis DB index（REDIS_URL 里带 /N）与其他项目隔离，
// 因此 key 本身不再加项目前缀，直接为 user:ad:limit:{user_id}:{creative_id} 等。
type RedisStore struct {
	client  *redis.Client
	timeout time.Duration
}

// NewRedis 从 REDIS_URL 构造疲劳度存储（DB index 由 REDIS_URL 的 /N 决定）。
func NewRedis(redisURL string) (*RedisStore, error) {
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}
	return &RedisStore{
		client:  redis.NewClient(opt),
		timeout: 100 * time.Millisecond, // 决策路径的 Redis 预算
	}, nil
}

// SetTimeout 调整 Redis 调用超时（默认 100ms）。
func (s *RedisStore) SetTimeout(d time.Duration) { s.timeout = d }

// Ping 校验连通性（启动探活）。
func (s *RedisStore) Ping(ctx context.Context) error { return s.client.Ping(ctx).Err() }

// Close 关闭连接。
func (s *RedisStore) Close() error { return s.client.Close() }

func (s *RedisStore) limitKey(userID, creativeID string) string {
	return "user:ad:limit:" + userID + ":" + creativeID
}
func (s *RedisStore) dailyKey(userID, creativeID string) string {
	return "user:ad:daily:" + userID + ":" + creativeID
}

// Check 读取两条计数（缺失视为 0）；任一达到上限即阻止。
func (s *RedisStore) Check(userID, creativeID string, cfg config.FatigueConfig) (bool, string) {
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()
	lim, err1 := s.client.Get(ctx, s.limitKey(userID, creativeID)).Int()
	dai, err2 := s.client.Get(ctx, s.dailyKey(userID, creativeID)).Int()
	// 仅对"非 redis.Nil 的真实错误"降级放行；key 不存在（redis.Nil）按 0 计。
	if err1 != nil && err1 != redis.Nil {
		return false, ""
	}
	if err2 != nil && err2 != redis.Nil {
		return false, ""
	}
	if lim >= cfg.WindowMax {
		return true, "window_exceeded"
	}
	if dai >= cfg.DailyMax {
		return true, "daily_exceeded"
	}
	return false, ""
}

// Record 两条计数各 +1；仅当从 0→1 时设置 EXPIRE（非滑动窗口语义：窗口从首次
// 观看起算整段时长）。
func (s *RedisStore) Record(userID, creativeID string, cfg config.FatigueConfig) {
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()
	if n, err := s.client.Incr(ctx, s.limitKey(userID, creativeID)).Result(); err == nil && n == 1 {
		s.client.Expire(ctx, s.limitKey(userID, creativeID), time.Duration(cfg.WindowMinutes)*time.Minute)
	}
	if n, err := s.client.Incr(ctx, s.dailyKey(userID, creativeID)).Result(); err == nil && n == 1 {
		s.client.Expire(ctx, s.dailyKey(userID, creativeID), 24*time.Hour)
	}
}

// 编译期接口实现检查。
var _ Store = (*RedisStore)(nil)
