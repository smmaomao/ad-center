# Redis 操作清单

> 排查 Redis 相关问题（首请求延迟、多余命令、key 对账）时的速查表。
> 最后更新：2026-09-22

## 总览

业务上共 **6 个子系统** 碰 Redis，全部走同一个 `REDIS_URL`（Upstash SG，单端点），各自一个独立的 go-redis client。另有 1 个纯观测 hook，不产生任何 Redis 命令。

| 子系统 | 文件 | key 前缀 / hash-tag | 启动语义 |
|---|---|---|---|
| 预算控制 | `internal/budget/redis.go` | `{bud}:` `{wal}:` | fail-loud（连不上启动退出）|
| 频控 | `internal/frequency/redis.go` | `{fr}:` | fail-loud |
| 事件队列 | `internal/queue/redis.go` | Streams（stream=`events`/group/consumer）| fail-loud |
| 决策缓存 | `internal/cache/redis.go` | `decision:` | 降级 Noop（**生产 0 命令，见 §4**）|
| Bid 登记表 | `internal/api/client.go` (`redisBidStore`) | `adcenter:bid:` | 降级内存 |
| 一次性 nonce | `internal/api/nonce.go` (`redisNonceStore`) | `adcenter:nonce:` | 降级内存 |
| *(观测)* | `internal/redislog/hook.go` | — | 只记日志，不下命令 |

> 前缀由 `cmd/server/main.go` 传空串决定（budget/frequency/cache 均传 `""`），只有 bid/nonce 在代码里硬编码 `adcenter:`。因此早先文档写的 `adcenter:decision:` 实际是 `decision:<sha256hex>`。
>
> **REDIS_LOG**：日志 hook **默认关闭**（每条命令一行 Info，生产常开太吵）。排查时设 `REDIS_LOG=1` 打开，排查完关掉。

---

## 各模块具体 Redis 操作

### 1. 预算控制 `budget/redis.go`（`{bud}` / `{wal}`）
- 读：`Stats`（Lua 跨日重置 → 读 `budget`/`spent`）、`StatsAll`（BatchStats，决策热路径 1 次/请求，跨样式只取 1 次）、`WalletBalance`（EXISTS + HGET 钱包余额，按广告主预取一次；**读走进程内短缓存** `WALLET_CACHE_TTL`，命中则 0 命令）
- 写：`TryDeduct`（Lua 原子扣费）、`WalletDeduct`（Lua 扣总钱包）、`WalletCredit`（HINCRBYFLOAT 充值）
- 后台对账：`SyncBalances`（**单个 Lua** `syncBalancesScript`）、`SyncWallets`（pipeline）、`sweepDeleted`（`SMEMBERS {bud}:ids` → `DEL` + `SREM`）、`sweepWallets`（`SMEMBERS {wal}:ids` → `DEL` + `SREM`）；两者都刻意不做 SCAN（Upstash 按请求计费）
  - **对账已脚本化**：原来 pipeline 里每 campaign 3~4 条命令 ≈ 3N 次请求/轮；预算键/dayKey/idsKey 全共享 `{bud}` hash tag（同 slot，与 `statsAllScript` 同理），故整体合成 1 个 EVALSHA —— 请求数 ≈3N+2 → 1
  - Hash 里的 `day` 字段已移除：原来每轮都写，但三个 Lua 脚本从不读它（跨日只判全局 `{bud}:day`）
- 命令族：EVALSHA、HGET / HSET / HSETNX / SADD / SMEMBERS / DEL / SREM / EXISTS / HINCRBYFLOAT、GET / SETNX（dayKey）

### 2. 频控 `frequency/redis.go`（`{fr}`）
- ZSET 滑动窗口，key：`{fr}:{app}:{dev}:adv:{campaign}`、`:recent:{creative}`；另有 `{fr}:…:recent:{creative}:seq` 计数器（String，由 `INCR` 隐式创建）
- 批量只读 `CheckBatch`（`BatchChecker`）：一次 Lua 往返完成 N 个候选的检查；返回 nil 时调用方回退逐条 `Check`，fail-open 语义不变
- **跨样式合并**：`/v1/ad/list` 走 `engine.DecideStyles`，先把 5 个样式的候选取并集、按 (campaign, creative) 去重，再**只发一次 `CheckBatch`**。依据：频控键里**不含 style**，逐样式检查会把同一对键重复读最多 5 遍（热路径 5 → 1）
- `:seq` 计数器已补 `EXPIRE`：原来 `EXPIRE` 只加在两个 ZSET 上，seq 键建了永不淘汰，是唯一无限增长的 key 族（基数 = app × device × creative）
- `Check`（只读）、`Record`（仅记账）；`CheckSlot` / `RecordSlot` / `CheckAndIncr` 已下线（无生产调用方）
- 命令族：EVALSHA、EXPIRE

### 3. 事件队列 `queue/redis.go`（Redis Streams）
- 写：`Publish` → 攒批 `flush` → `XADD`（pipeline，`MAXLEN≈20万` 裁剪）
- 读：`Run` 消费协程 `XReadGroup BLOCK`（空闲时每 ~30s 一次往返，返回 `nil` 即正常）→ 成功 `XAck`；消费失败不 ACK，留 PEL 重试
- **PEL 回收自适应退避**：`reclaim` 原来每 10s 无条件一次 `XAUTOCLAIM`（≈8640 条/天/实例，绝大多数是空转）。现连续空转则间隔翻倍至 60s 上限，一旦真的回收到消息立刻回到 10s；`minIdle=30s` 保证最坏 90s 内孤儿消息仍会被认领
- 监控：`XLen`、`XPending`（看积压，不看 Buffered）
- 命令族：XADD / XREADGROUP / XACK / XAUTOCLAIM / XLen / XPending / XGroupCreateMkStream

### 4. 决策缓存 `cache/redis.go`（`decision:`）
- **生产路径 0 命令、0 key**：唯一调用方 `resolveDecision`（`decision.go`）属于已下线的 `/v1/ad/req`，只有 `decision_cache_test.go` 会调它
- 兜底配置 `DefaultDecisionCacheConfig` 已改为 **Enabled=false**（原先 true —— 一旦有人把它接到 `/v1/ad/list`，热路径会无预警多出 GET+SET）。已在 settings 表配过 `decision_cache` 的实例不受影响
- `main.go` 仍建 client 并 Ping 一次（无害），但不产生命令

### 5. Bid 登记表 `api/client.go`（`redisBidStore`）（`adcenter:bid:`）
- 写：`PutBatch`（**一个 EVALSHA 批量 SET N 条**，原为每条下发一次 SET）；单条 `Put` 保留
- 读：`Get`（GET 反查下发上下文，用于归因/计费回传）
- `Consume`（防 bid_id 重放）：原 `SADD` + `EXPIRE` 两次串行，现合成 `bidConsumeScript` 一次往返
- 降级内存

### 6. 一次性 nonce `api/nonce.go`（`adcenter:nonce:`）
- `Issue`：`SETNX`（value=appID，TTL 默认 5min，`NONCE_TTL` 可配）
- `Consume`：`consumeScript` 一次 EVALSHA 完成 EXISTS+GET+DEL（原子核销）
- 降级内存；**该 client 也已挂 `redislog`**（此前遗漏，导致 nonce 命令在日志里不可见）

### 7. 日志 hook `redislog/hook.go`
- 纯观测：给每个 client 挂 `ProcessHook` / `ProcessPipelineHook`，打印每条命令的 `cmd / key / dur_ms / err`。**本身不下任何 Redis 命令**。默认关闭，`REDIS_LOG=1` 打开。

---

## 排查要点

- **请求路径实际打到 Redis 的**：预算快照（`StatsAll` 1 次）、频控（`CheckBatch` 1 次，跨样式合并后）、bid 登记（`PutBatch` 1 次）、队列 `XADD`（事件入队，攒批摊薄）。其余都在**后台对账 / 消费协程**里。决策缓存不参与（见 §4）。
- `ad/list` 的 `metrics` 步骤是本地 `*metrics.Agg`（内存聚合），**不走 Redis**。
- 6 个 client 共用一个 Upstash SG 端点，连接都是**懒建**的——首次执行命令才建连（DNS + TLS ≈ 2.4s）。所以部署后 30s 内的首个请求会付一次冷建连代价，之后全部 < 6ms。日志里那条 `xreadgroup ... dur_ms≈30004` 是后台阻塞读，与请求延迟无关，可忽略。
- 对账 key：`{bud}:ids`、`{wal}:ids`（SMEMBERS 枚举）、`{fr}:*`、`adcenter:decision:*`、`adcenter:bid:*`。

## 命令量模型（Upstash 按命令计费）

**命令数 ≈ 请求数 × 每请求命令数，与曝光量无直接关系。** 10-12 实测 42 万条/天对应后台 6,853 曝光，就是因为这个。

优化前每次 `/v1/ad/list` 的固定开销：

| 步骤 | 命令数 |
|---|---|
| `BudgetSnapshot` → `StatsAll` | 1（Lua 批量，已最优） |
| `WalletBalances`（遍历全部广告主） | 广告主数 ×1（未启用钱包只 EXISTS；启用则 EXISTS+HGET） |
| 频控 | **5 → 1**（原每样式一次 `CheckBatch`；现跨样式候选并集去重后一次） |
| 每条下发 bid 登记 | **N → 1**（默认 count=5、上限 20 条 SET；现 `PutBatch` 一个 EVALSHA） |

> 注意计费口径：Upstash 按**请求**计费——一个 EVALSHA（内含 N 条 Redis 操作）算 1 次；
> pipeline 里 N 条命令算 N 次。所以"合并成 Lua"是真省钱，pipeline 只降延迟。

典型 count=5：每请求约 **11 → 4** 次。

事件路径：`impression` ≈ 6 → 5（`Consume` 的 SADD+EXPIRE 合成 1 次）、`click` ≈ 5、`video-complete` ≈ 2。
后台：`SyncBalances` ≈3N+2 → **1** 次/轮（单 Lua）、`SyncWallets` 仍为 pipeline（2M+2）、`XAUTOCLAIM` 1 次/10~60s（自适应）、`XREADGROUP` 1 次/30s。

已做的削减（累计）：

1. **频控批量化**：`BatchChecker` + `checkBatchScript`，N 个候选合成 1 条 Lua（键都在 `{fr}` hash tag 下，Cluster 同 slot）。失败自动回退逐条 `Check`，fail-open 语义不变。
2. **频控跨样式合并**：`engine.DecideStyles` 把 5 个样式的候选并集去重后只查一次（频控键不含 style）。
3. **bid 登记批量化**：`PutBatch` 一个 EVALSHA 写 N 条；`Consume` 的 SADD+EXPIRE 合成单脚本。
4. **对账脚本化**：`SyncBalances` 单 Lua（≈3N+2 → 1）；并移除写了从不读的 `day` 字段。
5. **钱包余额进程内缓存**：`budget.Redis` 读走短 TTL 缓存（`WALLET_CACHE_TTL`，默认 2s，0=关闭），写路径主动失效。
6. **对账间隔 60s → 300s**（`cmd/server/main.go`）：NOTIFY 是主通道，reconcile 只兜底断线丢失。
7. **PEL 回收自适应退避**：空转时 10s → 60s（≈8640 → ≤1440 条/天/实例）。
8. **`:seq` 补 TTL**：止住唯一无限增长的 key 族。
9. **决策缓存默认关闭**：消除"接上就翻倍"的陷阱（生产路径本就 0 命令）。
10. **`redislog` 默认关闭**：不再每条命令刷一行 Info。

---

## 内存与淘汰策略（实例侧必配）

代码侧只能保证"每个 key 都设了显式 TTL"（软约束 ≤7 天，`stateKeyTTL`）；一旦漏设就会无限增长（历史上 `:seq` 就是例子，基数 app × device × creative）。因此**实例侧必须配兜底**：

- `maxmemory-policy` 建议 **`allkeys-lru`**：所有 key 都由应用显式管理 TTL，LRU 淘汰不会误伤"无 TTL 的常驻状态"；内存打满时按 LRU 淘汰，优于写入失败。
- `maxmemory` 按实例规格设置，留 20~30% 余量。
- 排查：`MEMORY USAGE <key>` 看大 key。`SCAN` **只用于人工排查**，不要放进定时任务——Upstash 按请求计费，每 500 key 一页非常贵；代码里的 sweep 一律走 `{bud}:ids` / `{wal}:ids` 集合枚举，正是这个原因。
