package budget

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis 预算控制器（路线 B，ARCHITECTURE.md §5.3.1 / SCALING.md §6）。
//
// 与 Memory 语义完全一致（跑同一套 contract_test），差别只在状态存放位置：
//   - 状态：Redis Hash {bud}:{advertiserID}，字段 budget / spent / day
//   - 跨日：与内存版同样**全局即时清零**（见 resetLocked 注释：惰性 per-key 重置
//     会让"昨日耗尽"的广告主在 Stats 里长期显示满额 → 引擎停投 → 再无扣费事件
//     触发重置 → 永久停投）
//   - 原子性：跨日重置 + 余额校验 + 累加由单个 Lua 脚本一次完成
//   - 降级 fail-open：Redis 不可用 → TryDeduct 返回 false（不扣钱）、
//     Stats 返回 (0,0)（engine 只读闸中 budget>0 才停投，故放行）
//
// 金额仍为 float64（与 Memory 一致）；整数 micros 化见 SCALING.md §7，待 M2 一并落地。
type Redis struct {
	client  *redis.Client
	prefix  string
	ledger  LedgerFn
	now     func() time.Time
	timeout time.Duration
}

// NewRedis 从 REDIS_URL 构造预算控制器（redis:// 与 rediss:// 均可）。
func NewRedis(redisURL, prefix string, ledger LedgerFn) (*Redis, error) {
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}
	return &Redis{
		client:  redis.NewClient(opt),
		prefix:  prefix,
		ledger:  ledger,
		now:     time.Now,
		timeout: 100 * time.Millisecond, // 决策/扣费路径的 Redis 预算，见 SCALING.md §5
	}, nil
}

// SetNow 注入时钟（测试用）。
func (r *Redis) SetNow(f func() time.Time) { r.now = f }

// SetTimeout 调整 Redis 调用超时（默认 100ms）。
func (r *Redis) SetTimeout(d time.Duration) { r.timeout = d }

// Ping 校验连通性（启动探活）。
func (r *Redis) Ping(ctx context.Context) error { return r.client.Ping(ctx).Err() }

// Close 关闭连接。
func (r *Redis) Close() error { return r.client.Close() }

// key 预算键。{bud} 是 hash tag：Cluster 下同前缀 key 落在同一 slot，
// 避免多 key 脚本/管道的 CROSSSLOT 错误（SCALING.md §5.2）。
func (r *Redis) key(advertiserID string) string {
	return r.prefix + "{bud}:" + advertiserID
}

// dayKey 全局预算日标记（跨日检测）。idsKey 已知广告主集合（跨日全局重置用）。
func (r *Redis) dayKey() string { return r.prefix + "{bud}:day" }

func (r *Redis) idsKey() string { return r.prefix + "{bud}:ids" }

func (r *Redis) today() string { return r.now().In(jakarta).Format("2006-01-02") }

// resetBlock 跨日重置的 Lua 片段（deduct / stats 共用）：
// 全局 day 与今日不符时，把已知广告主的 spent 全部归零并回传被重置的金额，
// 由 Go 侧补记 daily_reset 流水。多实例并发执行一次是幂等的（第二次读到 spent=0）。
const resetBlock = `
local resets = {}
if redis.call('GET', KEYS[2]) ~= ARGV[1] then
  local ids = redis.call('SMEMBERS', KEYS[3])
  for _, id in ipairs(ids) do
    local k = ARGV[2] .. id
    local sp = tonumber(redis.call('HGET', k, 'spent')) or 0
    if sp > 0 then
      resets[#resets + 1] = id
      resets[#resets + 1] = tostring(sp)
    end
    redis.call('HSET', k, 'day', ARGV[1], 'spent', '0')
  end
  redis.call('SET', KEYS[2], ARGV[1])
end
`

// deductScript 原子扣费：跨日重置 → 校验 → 累加。
//
// KEYS[1]=预算键，KEYS[2]=dayKey，KEYS[3]=idsKey；
// ARGV[1]=today(Jakarta YYYY-MM-DD)，ARGV[2]=key 前缀，ARGV[3]=amount。
// 返回 {ok, resets}：ok=1 扣费成功；resets 为本次跨日重置的 [id, amount, ...]
// （金额以字符串回传——Lua 数值回传会被截断为整数）。
var deductScript = redis.NewScript(resetBlock + `
if redis.call('EXISTS', KEYS[1]) == 0 then return {0, resets} end
local spent = tonumber(redis.call('HGET', KEYS[1], 'spent')) or 0
local budget = tonumber(redis.call('HGET', KEYS[1], 'budget')) or 0
local amount = tonumber(ARGV[3])
if amount < 0 then return {0, resets} end
if spent + amount > budget then return {0, resets} end
redis.call('HSET', KEYS[1], 'spent', tostring(spent + amount))
return {1, resets}
`)

// statsScript 读取预算快照（跨日先全局重置，与内存版 Stats 触发 rollover 一致）。
// 返回 {resets} 或 {resets, budget, spent}（键不存在时只有 resets）。
var statsScript = redis.NewScript(resetBlock + `
if redis.call('EXISTS', KEYS[1]) == 0 then return {resets} end
return {resets, redis.call('HGET', KEYS[1], 'budget'), redis.call('HGET', KEYS[1], 'spent')}
`)

func (r *Redis) TryDeduct(advertiserID string, amount float64) bool {
	ctx, cancel := context.WithTimeout(context.Background(), r.timeout)
	defer cancel()

	res, err := deductScript.Run(ctx, r.client,
		[]string{r.key(advertiserID), r.dayKey(), r.idsKey()},
		r.today(), r.prefix+"{bud}:", formatFloat(amount)).Result()
	if err != nil {
		return false // fail-open：扣费失败 = 不扣钱，事件照常确认
	}
	ok, resets := parseDeductResult(res)
	r.emitResets(resets)
	return ok
}

func (r *Redis) Stats(advertiserID string) (spent, budget float64) {
	ctx, cancel := context.WithTimeout(context.Background(), r.timeout)
	defer cancel()

	res, err := statsScript.Run(ctx, r.client,
		[]string{r.key(advertiserID), r.dayKey(), r.idsKey()},
		r.today(), r.prefix+"{bud}:").Result()
	if err != nil {
		return 0, 0 // fail-open：未知/失败视同无预算数据（engine 只读闸放行）
	}
	arr, ok := res.([]interface{})
	if !ok || len(arr) == 0 {
		return 0, 0
	}
	r.emitResets(parseResets(arr[0]))
	if len(arr) < 3 {
		return 0, 0 // 广告主未注册
	}
	return parseFloat(arr[2]), parseFloat(arr[1])
}

// emitResets 把跨日重置的金额补记 daily_reset 流水（与内存版 rollover 一致）。
func (r *Redis) emitResets(resets []resetEntry) {
	if r.ledger == nil {
		return
	}
	for _, e := range resets {
		r.ledger(e.id, "daily_reset", e.amount)
	}
}

// SyncBalances 用 DB 全量快照对齐 Redis 预算表（语义见 budget.Syncer 接口契约）。
//
// 两步 pipeline（先 EXISTS 再 HSET）而非多 key Lua：Cluster 下多 key 脚本会
// CROSSSLOT。任一步失败直接返回——下一轮（60s 对账）重试，宁可慢一拍也不可误覆盖。
func (r *Redis) SyncBalances(balances map[string][2]float64) {
	ctx, cancel := context.WithTimeout(context.Background(), r.timeout)
	defer cancel()

	ids := make([]string, 0, len(balances))
	for id := range balances {
		ids = append(ids, id)
	}

	pipe := r.client.Pipeline()
	exists := make([]*redis.IntCmd, len(ids))
	for i, id := range ids {
		exists[i] = pipe.Exists(ctx, r.key(id))
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return
	}

	today := r.today()
	pipe = r.client.Pipeline()
	pipe.SetNX(ctx, r.dayKey(), today, 0) // 确立当日（已存在则不动）
	for i, id := range ids {
		b := balances[id]
		k := r.key(id)
		pipe.SAdd(ctx, r.idsKey(), id) // 纳入跨日重置集合
		if exists[i].Val() > 0 {
			pipe.HSet(ctx, k, "budget", formatFloat(b[0])) // 已存在：只刷新上限，不动 spent
			continue
		}
		pipe.HSet(ctx, k, "budget", formatFloat(b[0]), "spent", formatFloat(b[1]), "day", today)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return
	}
	r.sweepDeleted(ctx, balances)
}

// sweepDeleted 清理 DB 中已不存在（软删）的广告主状态，避免无主 key 常驻。
func (r *Redis) sweepDeleted(ctx context.Context, keep map[string][2]float64) {
	const tag = "{bud}:"
	var cursor uint64
	for {
		keys, next, err := r.client.Scan(ctx, cursor, r.prefix+tag+"*", 500).Result()
		if err != nil {
			return
		}
		var del []string
		for _, k := range keys {
			id := strings.TrimPrefix(k, r.prefix+tag)
			// day / ids 是元数据键，不参与清理
			if id == "day" || id == "ids" {
				continue
			}
			if _, ok := keep[id]; !ok {
				del = append(del, k)
				_ = r.client.SRem(ctx, r.idsKey(), id).Err()
			}
		}
		if len(del) > 0 {
			_ = r.client.Del(ctx, del...).Err()
		}
		cursor = next
		if cursor == 0 {
			return
		}
	}
}

// HourlyCalibrate 每小时平滑校准记账。多实例下各实例都会记一条，
// 收敛为单实例 leader 执行是 M3 待办（SCALING.md §8）。
func (r *Redis) HourlyCalibrate() error {
	if r.ledger != nil {
		r.ledger("", "calibrate", 0)
	}
	return nil
}

type resetEntry struct {
	id     string
	amount float64
}

func formatFloat(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

func parseFloat(v any) float64 {
	switch x := v.(type) {
	case string:
		f, _ := strconv.ParseFloat(x, 64)
		return f
	case float64:
		return x
	case int64:
		return float64(x)
	default:
		return 0
	}
}

// parseResets 解析 [id, amount, ...] 扁平数组（Lua 以字符串回传金额）。
func parseResets(v any) []resetEntry {
	arr, ok := v.([]interface{})
	if !ok {
		return nil
	}
	out := make([]resetEntry, 0, len(arr)/2)
	for i := 0; i+1 < len(arr); i += 2 {
		id, isStr := arr[i].(string)
		if !isStr {
			continue
		}
		out = append(out, resetEntry{id: id, amount: parseFloat(arr[i+1])})
	}
	return out
}

// parseDeductResult 解析 {ok, resets}。
func parseDeductResult(v any) (ok bool, resets []resetEntry) {
	arr, isArr := v.([]interface{})
	if !isArr || len(arr) < 2 {
		return false, nil
	}
	if n, isNum := arr[0].(int64); isNum {
		ok = n == 1
	}
	return ok, parseResets(arr[1])
}

// 编译期接口实现检查。
var (
	_ Ctrl   = (*Redis)(nil)
	_ Syncer = (*Redis)(nil)
)
