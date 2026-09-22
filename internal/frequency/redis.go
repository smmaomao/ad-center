package frequency

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"

	"adcenter/internal/redislog"
)

// RedisStore 频控的 Redis 实现（路线 B，ARCHITECTURE.md §5.3.1 / SCALING.md §6）。
//
// 与 MemoryStore 语义一致（跑同一套 contract_test），状态改为 ZSET：
//   - 广告位级  {fr}:{app}:{dev}:slot:{slot}    —— 最小间隔 + 24h 日频控
//   - 广告主级  {fr}:{app}:{dev}:adv:{adv}      —— 多窗口滑动频控（跨广告位共享）
//   - 疲劳序列  {fr}:{app}:{dev}:recent:{slot}  —— 最近 N 次下发的广告主（member 前缀 = 广告主 ID）
//
// member 为「广告主ID:序号」（序号由 INCR 计数器生成，保证唯一），score = 毫秒时间戳。
// 过期由 EXPIRE(maxWindow) 承担，取代内存版的 Prune 后台清理。
// 错误一律 fail-open（放行），与 §5.3.1 的降级策略一致。
type RedisStore struct {
	client    *redis.Client
	prefix    string
	maxWindow time.Duration
	timeout   time.Duration
}

// NewRedis 从 REDIS_URL 构造频控存储。maxWindow 应 ≥ 最大窗口长度（建议 24h）。
func NewRedis(redisURL, prefix string, maxWindow time.Duration) (*RedisStore, error) {
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}
	client := redis.NewClient(opt)
	redislog.Attach(client)
	return &RedisStore{
		client:    client,
		prefix:    prefix,
		maxWindow: maxWindow,
		timeout:   100 * time.Millisecond, // 决策路径的 Redis 预算
	}, nil
}

// SetTimeout 调整 Redis 调用超时（默认 100ms）。
func (s *RedisStore) SetTimeout(d time.Duration) { s.timeout = d }

// Ping 校验连通性（启动探活）。
func (s *RedisStore) Ping(ctx context.Context) error { return s.client.Ping(ctx).Err() }

// Close 关闭连接。
func (s *RedisStore) Close() error { return s.client.Close() }

func (s *RedisStore) slotKey(appID, deviceID, slotID string) string {
	return s.prefix + "{fr}:" + appID + ":" + deviceID + ":slot:" + slotID
}

func (s *RedisStore) advKey(appID, deviceID, advertiserID string) string {
	return s.prefix + "{fr}:" + appID + ":" + deviceID + ":adv:" + advertiserID
}

func (s *RedisStore) recentKey(appID, deviceID, slotID string) string {
	return s.prefix + "{fr}:" + appID + ":" + deviceID + ":recent:" + slotID
}

// checkSlotScript 只读检查广告位额度（间隔 + 24h 日频控），不写状态。
//
// KEYS[1]=slotKey；ARGV: 1=nowMs, 2=intervalMs, 3=dailyLimit, 4=count。
var checkSlotScript = redis.NewScript(`
local now = tonumber(ARGV[1])
local count = tonumber(ARGV[4])
if count <= 0 then return 1 end
local interval = tonumber(ARGV[2])
if interval > 0 then
  local last = redis.call('ZREVRANGE', KEYS[1], 0, 0, 'WITHSCORES')
  if last[2] and (now - tonumber(last[2])) < interval then return 0 end
end
local daily = tonumber(ARGV[3])
if daily > 0 then
  if redis.call('ZCOUNT', KEYS[1], now - 86400000, now) + count > daily then return 0 end
end
return 1
`)

// recordSlotScript 记录 n 次下发（滑动窗口裁剪 + TTL）。
var recordSlotScript = redis.NewScript(`
local now = tonumber(ARGV[1])
local n = tonumber(ARGV[2])
local maxWindow = tonumber(ARGV[3])
local ttl = math.ceil(maxWindow / 1000)
for i = 1, n do
  local seq = redis.call('INCR', KEYS[1] .. ':seq')
  redis.call('ZADD', KEYS[1], now, now .. ':' .. seq)
end
redis.call('EXPIRE', KEYS[1] .. ':seq', ttl)
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', now - maxWindow)
redis.call('EXPIRE', KEYS[1], ttl)
return n
`)

// checkAndIncrScript 原子「疲劳检查 → 多窗口检查 → 记账」。
//
// KEYS[1]=recentKey，KEYS[2]=advKey；
// ARGV: 1=nowMs, 2=advertiserID, 3=fatigueN, 4=maxWindowMs, 5=窗口数,
//
//	其后每对 (windowMs, maxCount)。
var checkAndIncrScript = redis.NewScript(`
local now = tonumber(ARGV[1])
local advID = ARGV[2]
local fatigueN = tonumber(ARGV[3])
local maxWindow = tonumber(ARGV[4])
local wcount = tonumber(ARGV[5])
if fatigueN > 0 then
  local recent = redis.call('ZREVRANGE', KEYS[1], 0, fatigueN - 1)
  local pfx = advID .. ':'
  local plen = string.len(pfx)
  for _, m in ipairs(recent) do
    if string.sub(m, 1, plen) == pfx then return 0 end
  end
end
local i = 6
for _ = 1, wcount do
  local wms = tonumber(ARGV[i])
  local maxc = tonumber(ARGV[i + 1])
  i = i + 2
  if wms > 0 and maxc > 0 then
    if redis.call('ZCOUNT', KEYS[2], now - wms, now) >= maxc then return 0 end
  end
end
local seq = redis.call('INCR', KEYS[1] .. ':seq')
local member = advID .. ':' .. seq
local ttl = math.ceil(maxWindow / 1000)
redis.call('ZADD', KEYS[1], now, member)
redis.call('ZREMRANGEBYRANK', KEYS[1], 0, -11) -- 仅保留最近 10 条（疲劳窗口上限）
redis.call('EXPIRE', KEYS[1], ttl)
redis.call('ZADD', KEYS[2], now, member)
redis.call('ZREMRANGEBYSCORE', KEYS[2], '-inf', now - maxWindow)
redis.call('EXPIRE', KEYS[2], ttl)
return 1
`)

// checkOnlyScript 只读检查（疲劳 + 多窗口），不写状态。与 checkAndIncrScript 前段一致。
var checkOnlyScript = redis.NewScript(`
local now = tonumber(ARGV[1])
local advID = ARGV[2]
local fatigueN = tonumber(ARGV[3])
local wcount = tonumber(ARGV[4])
if fatigueN > 0 then
  local recent = redis.call('ZREVRANGE', KEYS[1], 0, fatigueN - 1)
  local pfx = advID .. ':'
  local plen = string.len(pfx)
  for _, m in ipairs(recent) do
    if string.sub(m, 1, plen) == pfx then return 0 end
  end
end
local i = 5
for _ = 1, wcount do
  local wms = tonumber(ARGV[i])
  local maxc = tonumber(ARGV[i + 1])
  i = i + 2
  if wms > 0 and maxc > 0 then
    if redis.call('ZCOUNT', KEYS[2], now - wms, now) >= maxc then return 0 end
  end
end
return 1
`)

// recordOnlyScript 仅记账（与 checkAndIncrScript 后段一致）。
var recordOnlyScript = redis.NewScript(`
local now = tonumber(ARGV[1])
local advID = ARGV[2]
local maxWindow = tonumber(ARGV[3])
local seq = redis.call('INCR', KEYS[1] .. ':seq')
local member = advID .. ':' .. seq
local ttl = math.ceil(maxWindow / 1000)
redis.call('ZADD', KEYS[1], now, member)
redis.call('ZREMRANGEBYRANK', KEYS[1], 0, -11)
redis.call('EXPIRE', KEYS[1], ttl)
redis.call('ZADD', KEYS[2], now, member)
redis.call('ZREMRANGEBYSCORE', KEYS[2], '-inf', now - maxWindow)
redis.call('EXPIRE', KEYS[2], ttl)
return 1
`)

func (s *RedisStore) CheckSlot(appID, deviceID, slotID string, policy SlotPolicy, count int, now time.Time) bool {
	if count <= 0 {
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()

	res, err := checkSlotScript.Run(ctx, s.client, []string{s.slotKey(appID, deviceID, slotID)},
		now.UnixMilli(), policy.Interval.Milliseconds(), policy.DailyLimit, count).Result()
	if err != nil {
		return true // fail-open：Redis 不可用 → 放行，事后由对账修正
	}
	n, ok := res.(int64)
	return !ok || n == 1 // 解析异常也放行
}

func (s *RedisStore) RecordSlot(appID, deviceID, slotID string, n int, now time.Time) {
	if n <= 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()
	_ = recordSlotScript.Run(ctx, s.client, []string{s.slotKey(appID, deviceID, slotID)},
		now.UnixMilli(), n, s.maxWindow.Milliseconds()).Err()
}

func (s *RedisStore) CheckAndIncr(appID, deviceID, slotID, advertiserID string, policy AdvPolicy, now time.Time) bool {
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()

	args := make([]any, 0, 6+2*len(policy.Windows))
	args = append(args, now.UnixMilli(), advertiserID, policy.FatigueN,
		s.maxWindow.Milliseconds(), len(policy.Windows))
	for _, w := range policy.Windows {
		args = append(args, int64(w.WindowMinutes)*60_000, w.MaxCount)
	}
	res, err := checkAndIncrScript.Run(ctx, s.client,
		[]string{s.recentKey(appID, deviceID, slotID), s.advKey(appID, deviceID, advertiserID)},
		args...).Result()
	if err != nil {
		return true // fail-open
	}
	n, ok := res.(int64)
	return !ok || n == 1
}

func (s *RedisStore) Check(appID, deviceID, slotID, advertiserID string, policy AdvPolicy, now time.Time) bool {
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()

	args := make([]any, 0, 4+2*len(policy.Windows))
	args = append(args, now.UnixMilli(), advertiserID, policy.FatigueN, len(policy.Windows))
	for _, w := range policy.Windows {
		args = append(args, int64(w.WindowMinutes)*60_000, w.MaxCount)
	}
	res, err := checkOnlyScript.Run(ctx, s.client,
		[]string{s.recentKey(appID, deviceID, slotID), s.advKey(appID, deviceID, advertiserID)},
		args...).Result()
	if err != nil {
		return true // fail-open
	}
	n, ok := res.(int64)
	return !ok || n == 1
}

func (s *RedisStore) Record(appID, deviceID, slotID, advertiserID string, policy AdvPolicy, now time.Time) {
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()
	_ = recordOnlyScript.Run(ctx, s.client,
		[]string{s.recentKey(appID, deviceID, slotID), s.advKey(appID, deviceID, advertiserID)},
		now.UnixMilli(), advertiserID, s.maxWindow.Milliseconds()).Err()
}

// 编译期接口实现检查。
var _ Store = (*RedisStore)(nil)
