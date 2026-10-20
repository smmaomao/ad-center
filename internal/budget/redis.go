package budget

import (
	"context"
	"log/slog"
	"math"
	"strconv"
	"sync"
	"sync/atomic"
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
//
// 钱包（广告主总余额）不使用 Redis：真相在 DB（advertisers.wallet_balance），
// 消费端扣费同事务递减（GREATEST 兜底不扣负）、充值同事务递增。此处只维护一份
// 由 SyncWallets 从 DB 全量灌入的内存快照，供决策只读闸判断「余额是否归 0」。
// 扣减不预扣（WalletDeduct 恒返回 true），超扣由消费端 GREATEST 兜底到 0。
type Redis struct {
	client  *redis.Client
	prefix  string
	ledger  LedgerFn
	now     func() time.Time
	timeout time.Duration

	// 钱包余额内存快照：key = advertiserID，只含已启用钱包的广告主。
	// 未启用钱包（不在 map 中）按 MaxFloat64 处理 = 不受总余额限制。
	// 由 SyncWallets 全量覆盖，充值时 WalletCredit 增量更新。
	walletMu    sync.RWMutex
	walletCache map[string]float64

	// #7 扣费/预算查询失败的可观测性：Redis 故障时 TryDeduct 返回 false（不扣钱）、
	// Stats 返回 (0,0)（引擎闸 budget>0 不成立 → 放行），两者都导致「广告照投、
	// 钱收不到」的免费投放。fail-open 语义按决策保留，但故障必须可见：计数 +
	// 节流 WARN，便于事后按 ad_events 明细补账。
	deductFailed   atomic.Int64
	statsFailed    atomic.Int64
	lastDeductWarn atomic.Int64
	lastStatsWarn  atomic.Int64
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
// stateKeyTTL 预算/跨日标记等状态键的过期上限（用户要求：所有 Redis key ≤7 天）。
// 数据真相在 DB，SyncBalances 每 300s 重建并刷新此 TTL，故活跃 campaign
// 永不过期；停投/删除的 campaign 在 ≤7 天后自动清理，无需 sweep。
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
    redis.call('HSET', k, 'spent', '0')
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
    redis.call('HSET', k, 'spent', '0')
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
		// fail-open：扣费失败 = 不扣钱，事件照常确认（等于免费投放）。
		// #7：保留该语义，但必须可见——计数 + 节流 WARN，供事后按 ad_events 补账。
		r.deductFailed.Add(1)
		warnThrottled(&r.lastDeductWarn, 30*time.Second,
			"budget deduct failed: Redis 故障导致未扣费（免费投放），需按 ad_events 补账",
			"advertiser_id", advertiserID, "amount", amount,
			"deduct_failed_total", r.deductFailed.Load(), "err", err)
		return false
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
		// fail-open：未知/失败视同无预算数据（engine 只读闸 budget>0 才停投 → 放行）。
		// #7：保留该语义，但这是另一条免费投放路径，必须可见。
		r.statsFailed.Add(1)
		warnThrottled(&r.lastStatsWarn, 30*time.Second,
			"budget stats failed: Redis 故障导致预算闸放行（免费投放）",
			"advertiser_id", advertiserID,
			"stats_failed_total", r.statsFailed.Load(), "err", err)
		return 0, 0
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

// Failures 返回扣费 / 预算查询的累计失败次数（#7 可观测性：Redis 故障 → 免费投放）。
// fail-open 语义不变，仅让故障可见；供健康检查 / 监控接入，以及事后按 ad_events 补账。
func (r *Redis) Failures() (deduct, stats int64) {
	return r.deductFailed.Load(), r.statsFailed.Load()
}

// warnThrottled 节流告警：同类故障每 interval 最多打一条日志，避免 Redis 故障时
// 日志洪水刷爆（决策路径每请求都会触发扣费/查询）。
func warnThrottled(last *atomic.Int64, interval time.Duration, msg string, args ...any) {
	now := time.Now().UnixNano()
	prev := last.Load()
	if prev != 0 && now-prev < int64(interval) {
		return
	}
	if last.CompareAndSwap(prev, now) {
		slog.Default().Warn(msg, args...)
	}
}

// DumpSpent 返回 Redis 中各 campaign 的当日已耗真值（按 campaign 维度），用于写回 DB。
// 复用 StatsAll（含跨日重置）拿到全量 (budget, spent)，取 spent 部分。
func (r *Redis) DumpSpent() map[string]float64 {
	ctx, cancel := context.WithTimeout(context.Background(), r.timeout)
	defer cancel()
	ids, err := r.client.SMembers(ctx, r.idsKey()).Result()
	if err != nil || len(ids) == 0 {
		return nil
	}
	all := r.StatsAll(ids)
	out := make(map[string]float64, len(all))
	for id, v := range all {
		out[id] = v[0] // [0]=spent, [1]=budget
	}
	return out
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

	keys := make([]string, 0, len(ids)+2)
	keys = append(keys, r.dayKey(), r.idsKey())
	args := make([]any, 0, 2+len(ids)*3)
	args = append(args, r.today(), int64(stateKeyTTL.Seconds()))
	for _, id := range ids {
		b := balances[id]
		keys = append(keys, r.key(id))
		// 预算上限始终用同步值刷新；spent 由脚本用 HSETNX 仅在字段缺失时初始化，
		// 避免覆盖运行时累计值。原 day 字段每轮都写、但三个 Lua 脚本从不读它
		// （跨日只判全局 {bud}:day），已移除。
		args = append(args, id, formatFloat(b[0]), formatFloat(b[1]))
	}
	if err := syncBalancesScript.Run(ctx, r.client, keys, args...).Err(); err != nil {
		return // 下一轮（60s 对账）重试，宁可慢一拍也不可误覆盖
	}
	r.sweepDeleted(ctx, balances)
}

// syncBalancesScript 用**一个** EVALSHA 完成整轮预算对账写入。
//
// 原实现是 pipeline 里每个 campaign 3~4 条命令 + 2 条全局命令：Upstash 按请求计费，
// N 个 campaign 就是 ~3N+2 次请求/轮（每次 notify:campaigns/advertisers 与 300s
// 对账各触发一轮）。预算键、dayKey、idsKey 全部共享 {bud} hash tag（同一 slot），
// 与 statsAllScript 同理在 Cluster 下单脚本安全，故可整体合成一个脚本：
// 整轮对账的请求数从 ~3N+2 降到 1（命令条数不变，但计费与往返都按 1 次算）。
//
// KEYS[1]=dayKey，KEYS[2]=idsKey，KEYS[3..]=各任务预算键；
// ARGV[1]=today(Jakarta YYYY-MM-DD)，ARGV[2]=TTL 秒，其后每任务三元组
// (id, budget, spent)。
var syncBalancesScript = redis.NewScript(`
local ttl = tonumber(ARGV[2])
redis.call('SETNX', KEYS[1], ARGV[1]) -- 确立当日（已存在则不动）
redis.call('EXPIRE', KEYS[1], ttl)
local ids = {}
for i = 3, #KEYS do
  ids[#ids + 1] = ARGV[3 + (i - 3) * 3]
end
if #ids > 0 then redis.call('SADD', KEYS[2], unpack(ids)) end
for i = 3, #KEYS do
  local a = 3 + (i - 3) * 3
  local k = KEYS[i]
  redis.call('HSET', k, 'budget', ARGV[a + 1])
  redis.call('HSETNX', k, 'spent', ARGV[a + 2])
  redis.call('EXPIRE', k, ttl)
end
redis.call('EXPIRE', KEYS[2], ttl)
return #ids`)

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

// ---- 广告主总钱包（充值 - 扣费）----
//
// 不使用 Redis。真相在 DB（advertisers.wallet_balance）：
//   - 扣费：消费端 WriteEventBatch 同事务递减（GREATEST(balance-amt,0) 兜底不扣负）
//   - 充值：store.Recharge 同事务递增
//   - 读：SyncWallets 从 DB 全量灌入本进程内存快照，决策只读闸判断「余额是否归 0」
//
// WalletDeduct 恒返回 true（不预扣）：请求路径只靠 campaign 日预算闸（Redis），
// 钱包扣减交给消费端 DB 事务。超扣由 GREATEST 兜底到 0，不会扣成负数。

// WalletBalance 总余额；未启用钱包 → MaxFloat64（不受总余额限制）。
// 读内存快照，0 Redis 0 DB。
func (r *Redis) WalletBalance(advertiserID string) float64 {
	r.walletMu.RLock()
	bal, ok := r.walletCache[advertiserID]
	r.walletMu.RUnlock()
	if !ok {
		return math.MaxFloat64
	}
	return bal
}

// WalletDeduct 总余额扣减：恒返回 true。
// 不做预扣，扣减由消费端 DB 事务（buildWalletDebit）负责，GREATEST 兜底不扣负。
func (r *Redis) WalletDeduct(advertiserID string, amount float64) bool {
	return true
}

// WalletCredit 充值入账：更新内存快照（充值已由 store.Recharge 写 DB）。
func (r *Redis) WalletCredit(advertiserID string, amount float64) {
	if amount <= 0 {
		return
	}
	r.walletMu.Lock()
	r.walletCache[advertiserID] += amount
	r.walletMu.Unlock()
}

// SyncWallets 用 DB 全量快照覆盖内存钱包余额。
// 入参只含已启用钱包的广告主；不在入参中的（未启用/已删除）从内存移除 → 不受总余额限制。
func (r *Redis) SyncWallets(balances map[string]float64) {
	w := make(map[string]float64, len(balances))
	for id, b := range balances {
		w[id] = b
	}
	r.walletMu.Lock()
	r.walletCache = w
	r.walletMu.Unlock()
}

// 编译期接口实现检查。
var (
	_ Ctrl       = (*Redis)(nil)
	_ Syncer     = (*Redis)(nil)
	_ BatchStats = (*Redis)(nil)
)
