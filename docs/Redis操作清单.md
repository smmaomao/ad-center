# Redis 操作清单

> 排查 Redis 相关问题（首请求延迟、多余命令、key 对账）时的速查表。
> 最后更新：2026-09-22

## 总览

业务上共 **5 个子系统** 碰 Redis，全部走同一个 `REDIS_URL`（Upstash SG，单端点），各自一个独立的 go-redis client。另有 1 个纯观测 hook，不产生任何 Redis 命令。

| 子系统 | 文件 | key 前缀 / hash-tag | 启动语义 |
|---|---|---|---|
| 预算控制 | `internal/budget/redis.go` | `adcenter:{bud}:` `adcenter:{wal}:` | fail-loud（连不上启动退出）|
| 频控 | `internal/frequency/redis.go` | `adcenter:{fr}:` | fail-loud |
| 事件队列 | `internal/queue/redis.go` | Streams（stream/group/consumer）| fail-loud |
| 决策缓存 | `internal/cache/redis.go` | `adcenter:decision:` | 降级内存 |
| Bid 登记表 | `internal/api/client.go` (`redisBidStore`) | `adcenter:bid:` | 降级内存 |
| *(观测)* | `internal/redislog/hook.go` | — | 只记日志，不下命令 |

> **REDIS_LOG**：日志 hook 默认开启，开着只影响日志量、不影响 Redis 负载，排查时保留。

---

## 各模块具体 Redis 操作

### 1. 预算控制 `budget/redis.go`（`{bud}` / `{wal}`）
- 读：`Stats`（Lua 跨日重置 → 读 `budget`/`spent`）、`StatsAll`（BatchStats，决策热路径 1 次/请求，跨样式只取 1 次）、`WalletBalance`（EXISTS + HGET 钱包余额，按广告主预取一次）
- 写：`TryDeduct`（Lua 原子扣费）、`WalletDeduct`（Lua 扣总钱包）、`WalletCredit`（HINCRBYFLOAT 充值）
- 后台对账：`SyncBalances`（pipeline `SADD {bud}:ids` + `HSET`）、`SyncWallets`、`sweepDeleted`（`SMEMBERS {bud}:ids` → `DEL` + `SREM`）、`sweepWallets`（`SMEMBERS {wal}:ids` → `DEL` + `SREM`）
- 命令族：EVALSHA、HGET / HSET / SADD / SMEMBERS / DEL / SREM / EXISTS / HINCRBYFLOAT、GET / SETNX（dayKey）

### 2. 频控 `frequency/redis.go`（`{fr}`）
- ZSET 滑动窗口，key：`{fr}:{app}:{dev}:slot:{slot}`、`:adv:{adv}`、`:recent:{slot}`
- 读+写（Lua 原子：ZREMRANGEBYSCORE → ZCARD → 判断 → ZADD）：`CheckSlot`、`RecordSlot`、`CheckAndIncr`、`Check`（只读）、`Record`（仅记账）
- 命令族：EVALSHA、EXPIRE

### 3. 事件队列 `queue/redis.go`（Redis Streams）
- 写：`Publish` → 攒批 `flush` → `XADD`（pipeline，`MAXLEN≈20万` 裁剪）
- 读：`Run` 消费协程 `XReadGroup BLOCK`（空闲时每 ~30s 一次往返，返回 `nil` 即正常）→ 成功 `XAck`
- 监控：`XLen`、`XPending`（看积压，不看 Buffered）
- 命令族：XADD / XREADGROUP / XACK / XLen / XPending / XGroupCreateMkStream

### 4. 决策缓存 `cache/redis.go`（`decision:`）
- 读：`Get`（GET，命中则复用 `/v1/ad/decision` 决策结果）
- 写：`Set`（SET + EXPIRE 回填）
- 多实例共享；降级内存（连不上不退出）

### 5. Bid 登记表 `api/client.go`（`redisBidStore`）（`bid:`）
- 写：`Put`（生成 `bid_id` → `SET` 带 TTL，默认 30min）
- 读：`Get`（GET 反查下发上下文，用于归因/计费回传）
- 降级内存

### 6. 日志 hook `redislog/hook.go`
- 纯观测：给每个 client 挂 `ProcessHook` / `ProcessPipelineHook`，打印每条命令的 `cmd / key / dur_ms / err`。**本身不下任何 Redis 命令**。

---

## 排查要点

- **请求路径实际打到 Redis 的**：预算快照（`StatsAll`）、频控 `Check`、队列 `XADD`（事件入队）、bid 表 `Get`，以及仅 `/v1/ad/decision` 的决策缓存 `Get`/`Set`。其余都在**后台对账 / 消费协程**里。
- `ad/list` 的 `metrics` 步骤是本地 `*metrics.Agg`（内存聚合），**不走 Redis**。
- 5 个 client 共用一个 Upstash SG 端点，连接都是**懒建**的——首次执行命令才建连（DNS + TLS ≈ 2.4s）。所以部署后 30s 内的首个请求会付一次冷建连代价，之后全部 < 6ms。日志里那条 `xreadgroup ... dur_ms≈30004` 是后台阻塞读，与请求延迟无关，可忽略。
- 对账 key：`{bud}:ids`、`{wal}:ids`（SMEMBERS 枚举）、`{fr}:*`、`adcenter:decision:*`、`adcenter:bid:*`。
