package budget

import (
	"context"
	"math"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"adcenter/internal/redislog"
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
	client := redis.NewClient(opt)
	redislog.Attach(client)
	return &Redis{
		client:  client,
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
// stateKeyTTL 预算/钱包/跨日标记等状态键的过期上限（用户要求：所有 Redis key ≤7 天）。
// 数据真相在 DB，SyncBalances/SyncWallets 每 60s 重建并刷新此 TTL，故活跃广告主
// 永不过期；停投/删除的广告主在 ≤7 天后自动清理，无需 sweep。
const stateKeyTTL = 7 * 24 * time.Hour

// resetBlock 跨日重置的 Lua 片段（deduct / stats 共用）：
// 全局 day 与今日不符时，把已知广告主的 spent 全部归零并回传被重置的金额，
// 由 Go 侧补记 daily_reset 流水。多实例并发执行一次是幂等的（第二次读到 spent=0）。
const resetBlock = `
local resets = {}
local ttl = 604800
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
    redis.call('EXPIRE', k, ttl)
  end
  redis.call('SET', KEYS[2], ARGV[1])
  redis.call('EXPIRE', KEYS[2], ttl)
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

// statsAllScript 批量读取全量任务预算快照（决策热路径，把 N 次 statsScript 合并为 1 次）。
//
// KEYS[1]=dayKey，KEYS[2]=idsKey，KEYS[3..]=各任务预算键；
// ARGV[1]=today(Jakarta YYYY-MM-DD)，ARGV[2]=key 前缀。
// 返回 {resets, b1, s1, b2, s2, ...}：resets 为跨日重置的扁平 [id, amount, ...]，
// 其后与 KEYS[3..] 一一对应的 (budget, spent) 对，键不存在时为空串。
// 所有预算键共享 {bud} hash tag（同 slot），Cluster 下单脚本安全；Upstash 只计 1 次请求。
var statsAllScript = redis.NewScript(`
local resets = {}
local ttl = 604800
if redis.call('GET', KEYS[1]) ~= ARGV[1] then
  local pfx = ARGV[2]
  local ids = redis.call('SMEMBERS', KEYS[2])
  for _, id in ipairs(ids) do
    local k = pfx .. id
    local sp = tonumber(redis.call('HGET', k, 'spent')) or 0
    if sp > 0 then
      resets[#resets + 1] = id
      resets[#resets + 1] = tostring(sp)
    end
    redis.call('HSET', k, 'day', ARGV[1], 'spent', '0')
    redis.call('EXPIRE', k, ttl)
  end
  redis.call('SET', KEYS[1], ARGV[1])
  redis.call('EXPIRE', KEYS[1], ttl)
end
local out = {resets}
for i = 3, #KEYS do
  local k = KEYS[i]
  if redis.call('EXISTS', k) == 0 then
    out[#out + 1] = ''
    out[#out + 1] = ''
  else
    out[#out + 1] = redis.call('HGET', k, 'budget') or ''
    out[#out + 1] = redis.call('HGET', k, 'spent') or ''
  end
end
return out
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

// StatsAll 批量版 Stats（BatchStats）：单个 Lua 一次往返拿全量任务预算，
// 把决策热路径里 N 次 EVALSHA/请求 降为 1 次。语义与逐个 Stats 一致：
// 先做跨日全局重置（含 emitResets 记 daily_reset 流水）；未注册 / 读失败的
// 广告主**不在返回 map 中**，调用方按 (0, 0) 处理（fail-open 放行）。
func (r *Redis) StatsAll(ids []string) map[string][2]float64 {
	out := make(map[string][2]float64, len(ids))
	if len(ids) == 0 {
		return out
	}
	ctx, cancel := context.WithTimeout(context.Background(), r.timeout)
	defer cancel()

	keys := make([]string, 0, len(ids)+2)
	keys = append(keys, r.dayKey(), r.idsKey())
	for _, id := range ids {
		keys = append(keys, r.key(id))
	}
	res, err := statsAllScript.Run(ctx, r.client, keys, r.today(), r.prefix+"{bud}:").Result()
	if err != nil {
		return out // fail-open：与 Stats 一致
	}
	arr, ok := res.([]interface{})
	if !ok || len(arr) < 1 {
		return out
	}
	r.emitResets(parseResets(arr[0]))
	// arr[1..] 与 ids[0..] 一一对应的 (budget, spent) 对
	for i := 1; i+1 < len(arr) && (i-1)/2 < len(ids); i += 2 {
		id := ids[(i-1)/2]
		b, bok := arr[i].(string)
		if !bok || b == "" {
			continue // 键不存在（未注册）：不入 map
		}
		out[id] = [2]float64{parseFloat(arr[i+1]), parseFloat(b)}
	}
	return out
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

	today := r.today()
	pipe := r.client.Pipeline()
	pipe.SetNX(ctx, r.dayKey(), today, stateKeyTTL) // 确立当日（已存在则不动），并刷 7d TTL
	if len(ids) > 0 {
		// 跨日重置集合：一条批量 SADD 取代逐个 SADD（省 N-1 条命令/次对账）
		members := make([]interface{}, len(ids))
		for i, id := range ids {
			members[i] = id
		}
		pipe.SAdd(ctx, r.idsKey(), members...)
	}
	for _, id := range ids {
		b := balances[id]
		k := r.key(id)
		// 预算上限始终用同步值刷新；spent/day 仅在字段缺失时初始化（HSetNX），
		// 避免覆盖运行时累计值。相比先 Exists 探活再分支，省掉一次管道往返。
		pipe.HSet(ctx, k, "budget", formatFloat(b[0]))
		pipe.HSetNX(ctx, k, "spent", formatFloat(b[1]))
		pipe.HSetNX(ctx, k, "day", today)
		pipe.Expire(ctx, k, stateKeyTTL) // 刷 7d TTL，活跃广告主永不过期
	}
	pipe.Expire(ctx, r.idsKey(), stateKeyTTL) // 刷 7d TTL
	if _, err := pipe.Exec(ctx); err != nil {
		return
	}
	r.sweepDeleted(ctx, balances)
}

// sweepDeleted 清理 DB 中已不存在（软删）的广告主状态，避免无主 key 常驻。
//
// 实现走 {bud}:ids 集合（SyncBalances 每轮维护）枚举已知 id，**不做全量 SCAN**
// ——每次对账只多 1 条 SMEMBERS + 真有删除时的 DEL/SREM。Upstash 按请求计费，
// 全量 SCAN（每 500 key 一页）是对账里最贵的一条，且 7 天 TTL（stateKeyTTL）
// 本就是无主 key 的兜底，这里只是提前清理。
func (r *Redis) sweepDeleted(ctx context.Context, keep map[string][2]float64) {
	ids, err := r.client.SMembers(ctx, r.idsKey()).Result()
	if err != nil || len(ids) == 0 {
		return
	}
	var del []string
	srem := make([]interface{}, 0, len(ids))
	for _, id := range ids {
		if _, ok := keep[id]; ok {
			continue
		}
		del = append(del, r.key(id))
		srem = append(srem, id)
	}
	if len(del) > 0 {
		_ = r.client.Del(ctx, del...).Err()
		_ = r.client.SRem(ctx, r.idsKey(), srem...).Err()
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

// ---- 广告主总钱包（充值 - 扣费），与 Memory 同语义 ----
//
// 状态：Redis Hash {wal}:{advertiserID} 字段 balance。**键存在 = 已启用钱包**；
// 键不存在 = 未启用（不受总余额限制）—— 用 EXISTS 区分"未启用"与"余额为 0"。

func (r *Redis) walletKey(advertiserID string) string {
	return r.prefix + "{wal}:" + advertiserID
}

// walletIDsKey 已启用钱包的广告主集合（SyncWallets 维护，sweepWallets 枚举用），
// 与 walletKey 同 {wal} hash tag（同 slot，Cluster 兼容）。
func (r *Redis) walletIDsKey() string { return r.prefix + "{wal}:ids" }

// walletDeductScript 原子扣减总余额：
//
//	键不存在 → 返回 1（未启用，放行扣费，仍走 campaign 日预算闸）
//	余额不足 → 返回 0
//	否则扣减并返回 1
var walletDeductScript = redis.NewScript(`
if redis.call('EXISTS', KEYS[1]) == 0 then return 1 end
local bal = tonumber(redis.call('HGET', KEYS[1], 'balance')) or 0
local amount = tonumber(ARGV[1])
if amount < 0 or bal < amount then return 0 end
redis.call('HSET', KEYS[1], 'balance', tostring(bal - amount))
redis.call('EXPIRE', KEYS[1], 604800)
return 1
`)

// WalletBalance 总余额；未启用 / 读失败 → MaxFloat64（不限制，fail-open）。
func (r *Redis) WalletBalance(advertiserID string) float64 {
	ctx, cancel := context.WithTimeout(context.Background(), r.timeout)
	defer cancel()
	k := r.walletKey(advertiserID)
	n, err := r.client.Exists(ctx, k).Result()
	if err != nil || n == 0 {
		return math.MaxFloat64
	}
	v, err := r.client.HGet(ctx, k, "balance").Result()
	if err != nil {
		return math.MaxFloat64
	}
	return parseFloat(v)
}

// WalletDeduct 总余额扣减；未启用 / 失败放行（true）。
func (r *Redis) WalletDeduct(advertiserID string, amount float64) bool {
	ctx, cancel := context.WithTimeout(context.Background(), r.timeout)
	defer cancel()
	res, err := walletDeductScript.Run(ctx, r.client,
		[]string{r.walletKey(advertiserID)}, formatFloat(amount)).Result()
	if err != nil {
		return true // fail-open：Redis 不可用 → 放行（仍受 campaign 日预算闸）
	}
	n, ok := res.(int64)
	return !ok || n == 1
}

// WalletCredit 充值入账；键不存在时创建（充值即启用钱包）。
func (r *Redis) WalletCredit(advertiserID string, amount float64) {
	if amount <= 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), r.timeout)
	defer cancel()
	_ = r.client.HIncrByFloat(ctx, r.walletKey(advertiserID), "balance", amount).Err()
	_ = r.client.Expire(ctx, r.walletKey(advertiserID), stateKeyTTL).Err() // 刷 7d TTL
}

// SyncWallets 用 DB 快照覆盖钱包余额，并清理不再启用的钱包键。
func (r *Redis) SyncWallets(balances map[string]float64) {
	ctx, cancel := context.WithTimeout(context.Background(), r.timeout)
	defer cancel()
	pipe := r.client.Pipeline()
	if len(balances) > 0 {
		// 已启用钱包集合：一条批量 SADD（sweepWallets 枚举用，免全量 SCAN）
		members := make([]interface{}, 0, len(balances))
		for id := range balances {
			members = append(members, id)
		}
		pipe.SAdd(ctx, r.walletIDsKey(), members...)
	}
	pipe.Expire(ctx, r.walletIDsKey(), stateKeyTTL)
	for id, b := range balances {
		pipe.HSet(ctx, r.walletKey(id), "balance", formatFloat(b))
		pipe.Expire(ctx, r.walletKey(id), stateKeyTTL) // 刷 7d TTL
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return
	}
	r.sweepWallets(ctx, balances)
}

// sweepWallets 清理 DB 中未启用（已停用钱包/已删除）的广告主钱包键。
// 与 sweepDeleted 同思路：走 {wal}:ids 集合枚举，不做全量 SCAN。
func (r *Redis) sweepWallets(ctx context.Context, keep map[string]float64) {
	ids, err := r.client.SMembers(ctx, r.walletIDsKey()).Result()
	if err != nil || len(ids) == 0 {
		return
	}
	var del []string
	srem := make([]interface{}, 0, len(ids))
	for _, id := range ids {
		if _, ok := keep[id]; ok {
			continue
		}
		del = append(del, r.walletKey(id))
		srem = append(srem, id)
	}
	if len(del) > 0 {
		_ = r.client.Del(ctx, del...).Err()
		_ = r.client.SRem(ctx, r.walletIDsKey(), srem...).Err()
	}
}

// 编译期接口实现检查。
var (
	_ Ctrl       = (*Redis)(nil)
	_ Syncer     = (*Redis)(nil)
	_ BatchStats = (*Redis)(nil)
)
