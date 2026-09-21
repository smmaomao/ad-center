# 多实例与异步落库演进规划（SCALING）

> 配套文档：`ARCHITECTURE.md §5.3`（P1 升级路径 / §5.3.1 迁移手册）。
> 本文记录从「单实例 + 进程内状态」演进到「多实例无状态 + 队列削峰」的完整规划与实施状态。
> 状态：v1（2026-09-07）——M1 已实施，M2+ 待量级触发。

---

## 0. 背景与三条原则

**现状：** Fly Singapore 单实例，服务内有 4 处进程内状态（见 §1）。一旦 `fly scale count 2`，内存态必然多副本 → 预算超发、频控失效。

**目标：** 应用进程无状态（可任意扩缩容 / 滚动发版），状态外移到 Redis，写路径经队列削峰后批量落库。

**三条不可违背的原则：**

1. **请求热路径零 DB 写** —— 决策与回执只碰 Redis（Lua 原子操作），DB 只接受批量 / 聚合 / 异步写入。
2. **状态外移而非同步** —— 运行态（预算 / 频控）在 Redis 保持唯一副本 + Lua 原子性，不做实例间同步。
3. **Postgres 是真相** —— Redis 全部状态可由 `ad_events` / `budget_ledger` 回放重建；Redis 丢失可自愈，不是灾难。

---

## 1. 内存态清单：哪些必须外移

| # | 组件 | 现状 | 多实例后果 | 分布式方案 | 优先级 |
|---|---|---|---|---|---|
| 1 | 预算 `budget.Memory` | 进程内 map + 锁 | **预算超发 N 倍（资损）** | Redis Hash + Lua 原子扣减 | **P0（已完成）** |
| 2 | 频控 `frequency.Memory` | 进程内时间戳队列 | 频控形同虚设，用户看到 N 倍广告 | Redis ZSET + Lua | **P0（已完成）** |
| 3 | 事件队列 `eventWriter` | 进程内 channel(8192) | 队列满丢弃、进程重启丢一批、积压不可见 | Redis Streams + consumer group | P1（M2） |
| 4 | 实时指标 `metrics.Agg` | 内存分钟计数 | 计数分片，但落库是 UPSERT 累加 → DB 汇总正确，仅看板滞后 1 分钟 | 保持本地计数 + DB 汇总；秒级看板再上 Redis 聚合桶 | P2 |

**无需改动：** `config.Cache`（每实例副本 + LISTEN/NOTIFY + 60s 对账，幂等可复制）、`DecisionCache`（已 Redis）、`ClickResolver`（纯 DB 查询，无状态）。

---

## 2. 目标架构

```
【读路径】不能加队列，延迟预算 ~50ms
  /v1/ad/req → 引擎 → Redis Lua[频控检查 + 预算只读闸] → 200
                                                         └ 零 DB，Redis 单次往返

【写路径】全部经队列
  /v1/ad/event(曝光/点击) → Redis Lua[预算原子扣减] → XADD → 200
                                                          ↓
                                                 Stream / Topic（削峰填谷）
                                                          ↓
                                          consumer group（按 DB 能力匀速消费）
                                            ├→ ad_events       批量 INSERT
                                            ├→ budget_ledger   按(广告主, 窗口)聚合写一条
                                            └→ metrics          内存计数 → 每分钟 UPSERT
```

**收益：** 生产端只做一次 XADD（亚毫秒）；消费端限速 → 保护 Supabase 连接池（连接数是最稀缺资源）；ACK + PEL 保证至少一次；积压可观测可告警。

---

## 3. 消息队列选型

### 阶段 1（M2，零新增服务）：Redis Streams

已有 Redis 直接用：自带 consumer group、ACK/PEL、持久化、积压可见（`XLEN`）。当前量级（单实例 Streams 轻松几万条/秒）绰绰有余。

- 生产端**批量 XADD**（pipeline，每 10ms 或 100 条一批）——降 RTT，也降 Upstash 按请求计费的成本。
- 保留策略：`XADD MAXLEN ~ N`（约数小时~百万条），足够吸收峰值；更久的保留交给 Postgres/ClickHouse。
- 消费端**走原生 Redis 协议（TCP/TLS）的 `XREADGROUP BLOCK 30s COUNT 500` 长阻塞**，不是定时轮询：BLOCK 期间请求挂在服务端，空闲时 30 秒才一次往返（Upstash 按请求计费 → 空闲几乎不花钱）。**绝不能配 Upstash 的 REST 端点**：REST 是 HTTP 请求式、明确不支持阻塞版 XREAD/XREADGROUP，只能每秒轮询，请求数与延迟都会暴涨。客户端读超时必须 > BLOCK（Upstash 原文："Set the client/network timeout longer than the command timeout"）；go-redis 对带 BLOCK 的 XREADGROUP 会自动把读超时放宽为 `block+10s`，无需手动改 `ReadTimeout`。可用环境变量 `QUEUE_BLOCK` 覆盖阻塞时长。

### 阶段 2（M4，量级触发后）：Kafka 系

触发条件（任一）：事件持续 > 2~5k/s、需要 >24h 保留 / 重放、需要多个独立消费者组（实时看板 + 落库 + 风控各一份）。

| 场景 | 推荐 | 理由 |
|---|---|---|
| 与 Redis 统一账单 | **Upstash Kafka** | Kafka API、按量计费、起步低 |
| 吞吐成本优先 | **Redpanda Serverless** | Kafka 兼容，单位吞吐更便宜 |
| 延迟队列 / 复杂路由 / 死信 | CloudAMQP (RabbitMQ) | 语义丰富，扩展性弱于 Kafka |
| 极致轻量低延迟 | NATS JetStream (Synadia) | 轻、快，生态略小 |

不建议在 Fly 上自建 Kafka（运维成本远超收益）。迁移成本低：生产端换 client，消费端语义不变。

### 基础设施建议

- **Redis：Upstash**（Fly 官方集成、可选与 Fly 应用同区域、Serverless 按量、支持 Streams 与 Lua `EVAL`）。
- **同区域**：Fly 应用、Upstash、Supabase 三者同区域（印尼市场 → 新加坡 `sin`）。跨区域 RTT 会直接吃掉决策延迟预算。

---

## 4. 里程碑

| 里程碑 | 内容 | 状态 |
|---|---|---|
| M0 | 定 Upstash Redis + 应用同区域；key 前缀规范落地 | 待办 |
| M1 | 预算 / 频控 Redis 化（复用现有契约测试） | **已完成**（见 §6） |
| M2 | 事件写路径改 Redis Streams：生产批量 XADD + 消费 group 批量落库 + 计费流水聚合 | **已完成**（见 §7） |
| M3 | 可观测：队列长度 / lag、消费速率、丢弃数、Redis 延迟告警 | 待办 |
| M4 | 依据 M3 实际量级评估是否上 Kafka | 待办 |

**顺序不可颠倒**（§5.3.1）：状态先外移并验证，再 `fly scale count 2` 扩容。

---

## 5. 关键约束与坑

1. **不用 Redis DB index**：本地验证用过 `/2`，但 Upstash / Cluster 不支持多 DB。统一用 key 前缀 `adcenter:`，DB index 只能是本地开发约定。
2. **Hash tag 保 cluster 兼容**：多 key 的 Lua 脚本在 Cluster 下会 `CROSSSLOT`。所有 key 形如 `adcenter:{bud}:{id}` / `adcenter:{fr}:{app}:{dev}:...`，`{bud}` `{fr}` 强制同 slot。
3. **预算跨日**：Hash 里存 `day` 字段（Jakarta `YYYY-MM-DD`），Lua 内比对跨日则 `spent` 归零，并回传旧值记 `daily_reset` 流水——与内存版语义一致。key **不设 TTL**（key 过期 = 广告主"未注册" = 停投，宁可让 `SyncBalances` 显式删除）。
4. **降级 fail-open**（与 §5.3.1 一致）：Redis 超时/不可用 → `Stats` 返回 `(0,0)`（engine 只读闸中 `budget>0` 才停投，故放行）、`TryDeduct` 返回 false（不扣费）、频控放行。广告业务宁可短暂多放，不能全量无广告；事后从 Postgres 对账修正。
5. **RTT 合并**：决策热路径的「频控 + 预算」各自一次 Redis 往返（目标：后续合并进一个 Lua，降到 1 次）。
6. **幂等**：事件带唯一 id（`impression_id` / `click_id`），消费端 `ON CONFLICT DO NOTHING`；`budget_ledger` 聚合行用 (广告主, 窗口) 唯一键 + `ON CONFLICT DO UPDATE` 累加——应对客户端重试与归因方重投。
7. **Supabase 连接池**：走 Supavisor / pgBouncer transaction 模式，由消费者进程统一批量写，避免每实例各自高频写。
8. **Redis 端点只用 TCP**：`REDIS_URL` 必须是控制台里的 `rediss://xxx.upstash.io:6379`（原生 Redis 协议），**不是** `https://xxx.upstash.io`（REST）。REST 无 BLOCK，长跑的 Go 消费端会退化成轮询刷请求；也别在长跑服务里用 `@upstash/redis` 那类 REST SDK。

---

## 6. M1 实施说明（预算 / 频控 Redis 化）

### 开关

环境变量 `STATE_STORE`：

- `memory`（默认）：沿用进程内实现，单实例 / 本地开发。
- `redis`：预算与频控走 Redis，需 `REDIS_URL`；连接或 Ping 失败时**启动即报错退出**（不静默降级到内存版，避免"以为分布式、其实各管各的"这种最难排查的资损）。

### 预算 `internal/budget/redis.go`

- 结构：Hash `adcenter:{bud}:{advertiserID}`，字段 `budget` / `spent` / `day`（金额以字符串存储，规避 Lua 数值回传被截断为整数的问题）；另有元数据键 `{bud}:day`（全局预算日）与 `{bud}:ids`（已知广告主集合）。
- **跨日必须全局即时清零，不能用惰性 per-key 重置**：契约测试跑出来的真实差异——惰性方案下，昨日耗尽的广告主在 `Stats` 里长期返回 `spent=budget` → 引擎只读闸停投 → 再无扣费事件触发重置 → **该广告主永久停投**。现实现：`TryDeduct` / `Stats` 的 Lua 先比对全局 `day`，不符则遍历 `ids` 集合一次性清零并回传重置金额（多实例并发执行幂等）。
- `TryDeduct`：Lua 原子执行「跨日重置 → 余额校验 → 累加」，返回 `{ok, resets}`；被重置的金额由 Go 侧补记 `daily_reset` 流水（与内存版 rollover 一致）。
- `Stats`：同样先触发跨日重置，再读快照（与内存版 `Stats` 触发 rollover 的语义一致）。
- `SyncBalances`：两步 pipeline（先 `EXISTS` 再 `HSET`），**只用多 key pipeline 而非多 key 脚本**——Cluster 下多 key 脚本会 CROSSSLOT。已存在只刷 `budget`；新增按 DB 快照初始化；DB 中已不存在的 key 用 SCAN + DEL 清理。
- `HourlyCalibrate`：沿用内存版语义记 `calibrate` 流水（多实例会各记一条，M3 收敛为单实例 leader 执行）。
- 调用超时默认 100ms，可 `SetTimeout` 调整。

### 频控 `internal/frequency/redis.go`

- slot 级：`adcenter:{fr}:{app}:{dev}:slot:{slot}` ZSET（member 唯一序列，score = 毫秒时间戳）——支撑最小间隔（ZREVRANGE 取最后一条）与 24h 日频控（ZCOUNT）。
- 广告主级：`adcenter:{fr}:{app}:{dev}:adv:{adv}` ZSET——多窗口滑动计数（跨广告位共享）。
- 疲劳窗口：`adcenter:{fr}:{app}:{dev}:recent:{slot}` ZSET，member 前缀为广告主 ID，取最近 N 条比对；保留上限 10 条（`ZREMRANGEBYRANK`）。
- `CheckAndIncr` 单个 Lua 原子完成「疲劳 → 多窗口 → 记账」；member 序列由 `INCR` 计数器生成，保证唯一。
- 过期由 `EXPIRE`（= maxWindow）替代内存版的 `Prune` 后台清理；`Prune` 作为可选接口，仅内存版实现（`main` 用类型断言调用）。
- 错误一律 fail-open（放行），仅打日志。

### 契约测试

`budget` / `frequency` 的 `contract_test.go` 是接口级套件（§5.3.1 要求）。新增 `redis_contract_test.go`：设置 `TEST_REDIS_URL` 时**跑同一套用例**，未设置则 `t.Skip`（CI 无 Redis 也能过）。

```bash
TEST_REDIS_URL=redis://localhost:6379/3 go test ./internal/budget/... ./internal/frequency/...
```

### 验证记录（2026-09-07）

- 契约测试：预算 / 频控的 Redis 实现**跑同一套用例全绿**（`ok adcenter/internal/budget`、`ok adcenter/internal/frequency`）。
- 真实启动：`STATE_STORE=redis REDIS_URL=redis://localhost:6379/3` 启动，日志依次输出
  `budget store enabled (redis)` / `frequency store enabled (redis)` / `decision cache enabled (redis)`，`/healthz` 返回 ok。
- 键结构落库正确：

```
adcenter:{bud}:00000000-...-00b1   hash{budget=100, spent=0, day=2026-09-07}
adcenter:{bud}:day                 "2026-09-07"     ← 全局预算日（跨日检测）
adcenter:{bud}:ids                 set{广告主ID...}  ← 跨日全局重置用
```

- 全量回归 `go test ./...` 通过；`gofmt` / lint 干净。

---

## 7. M2 实施说明（事件队列 + 聚合落库）

### 开关

`QUEUE_DRIVER=memory|redis`（默认 memory），与 `STATE_STORE` 互不影响。
`redis` 模式下：事件经 Streams，`deduct` 流水**不再在请求路径逐笔写 DB**，改由消费端聚合。

### 队列 `internal/queue`

- `Backend` 接口：`Publish` / `Run(handler)` / `Stats` / `Close`，两种实现同一接口。
- Memory：进程内 channel（8192）+ 500 条 / 2s 攒批——保持迁移前行为。
- Redis：Streams `adcenter:events`，group `adcenter`，consumer = `hostname-pid`（多实例自动分摊）。
  - 生产端攒批后一次 pipeline `XADD`（不满批由 10ms ticker 兜底）
  - 消费端 `XREADGROUP`（count 500 / block 300ms）→ 批量落库 → `XACK`；
    处理失败不 ACK，消息留在 PEL 等重投
  - `MAXLEN ~ 200000` 裁剪。**监控看 Pending（消费是否卡住），不是 XLEN**（ACK 不删消息）

### 落库（消费端）

- 明细 `ad_events` 与聚合流水 `budget_ledger` 在**同一事务**内写入（`store.WriteEventBatch`）——
  消费是至少一次语义，分步写会在重投时留下"明细写了、流水没写"的半截状态。
- 聚合：批次内按 (广告主, app) 汇总金额与次数 → 一行 `op_type='deduct_agg'`，
  `ON CONFLICT (advertiser_id, day, hour_bucket) WHERE op_type='deduct_agg'` 累加（migration 000013）。
- 迁移坑：000013 除 `count` 列与部分唯一索引外，还必须放开 `budget_ledger.op_type`
  的检查约束，否则聚合行写入被 `23514` 拒绝（本地验证时实际踩到）。

### 验证记录（2026-09-07，本地单实例 + 本地 Redis / Postgres）

50 条 impression（CPM $5 → 单次 $0.005）端到端：

```
队列   XLEN=50  Pending=0（全部 ACK）
明细   impression rows=50 revenue=0.25
流水   deduct_agg amount=0.25 count=50      ← 50 条事件 → 1 条流水
预算   spent=0.25（预算 100）
失败   0
```

明细 / 流水 / 预算三方一致；同一 group 起两个进程可见消费自动分摊。

### 已知限制

- **ad_events 无事件级幂等**：消费者崩溃后 PEL 重投会产生重复明细行；
  聚合流水靠唯一键不会重复计金额。M3 补 `event_uid` 唯一键。
- 每实例一个消费者；消费能力不足优先加消费者（加实例），而不是无限调大批次。

---

## 8. 计费精度（流水聚合已随 M2 落地，整数化待办）

**已确认方向，待 M2 与队列改造一并落地：**

1. **金额最小单位整数化**：内部一律用 `int64` micros（$1 = 1,000,000 micros），`BillingAmount` 返回整数 micro 金额，`TryDeduct` 改整数累加 —— 彻底消除 float 累积误差（CPM $0.35 → 单次 350 micros）。M1 仍沿用 `float64` 接口，避免与状态外移混改。
2. **流水不每事件一条**：由消费者按 (广告主, 时间窗) 聚合成一条 `SUM + COUNT`，DB 写入量降 2~3 个数量级；`budget_ledger.amount` 列改 `numeric(18,6)`。
3. **决策请求的 `fill` 明细**：指标已被 `metrics_minute` 覆盖，该明细属冗余，改为可关闭 / 采样。

---

## 9. 分区维护（ad_events / decision_logs）

两张表都是声明式分区（`PARTITION BY RANGE (ts)`），按月分表 `*_YYYY_MM`。
000001 只预建了「当前月 + 未来 5 个月」，**没有自动维护**——分区到期月份不存在时，
写入会直接失败（`23P01 no partition of relation found for row`），属于只在跨月那一刻
才暴露的定时炸弹。

**现在的保障：**

1. `migrations/000014_precreate_partitions.up.sql`：一次性补建未来 12 个月（幂等，可重复执行）。
2. Go 侧每日 03:00（Jakarta）定时任务 `store.EnsurePartitions(ctx, 2)`：保证「下月 + 下下月」就绪，留一天缓冲。启动时**不**执行（分区已由 000014 铺满一年）。

**多实例并发**（同一个定时任务会在每台实例各跑一次）：

| 层 | 手段 |
|---|---|
| 1 | `CREATE TABLE IF NOT EXISTS ... PARTITION OF`：已存在则跳过 |
| 2 | `pg_advisory_xact_lock`：同一时刻只有一个实例建分区。建分区需对父表加 ACCESS EXCLUSIVE 锁，串行化后 `IF NOT EXISTS` 的判断才可靠 |
| 3 | 忽略 `duplicate_table (42P07)`，兜住极端竞态 |

实测：8 个实例并发调用 `EnsurePartitions` → **失败 0 / 8**。

> 上线提醒：000014 需要在 Supabase 上执行一次（与既有 migration 同一流程），
> 否则在每日任务生效前仍只有 6 个月窗口。

### 分区 TTL 清理（删除保留窗口之外的旧子分区）

预建解决"往前建"，但分区只增不删会无限堆积。新增每日 04:00（Jakarta）定时任务
`store.DropOldPartitions(ctx, retainMonths)`，与 ⑥' 配对做"往后清"。

**保留期**：`retainMonths` = 保留最近多少个月（含当前月），默认 **13**（覆盖一个
完整年度 + 跨年同比），由环境变量 `PARTITION_RETENTION_MONTHS` 覆盖。
例：`retainMonths=13` → 最旧保留月 = 当前月 - 12 个月，更早的子分区全部 DROP。

**安全性（绝不误删）**：

| 层 | 手段 |
|---|---|
| 1 | 只扫 `pg_inherits` 的真实子分区，且名称必须匹配 `^<tbl>_[0-9]{4}_[0-9]{2}$` —— 父表本身、非分区表、命名不合规的分区都不会被选中 |
| 2 | 只删"整月都早于保留窗口"的分区（分区上界 <= keep_from），保留窗口内的分区原封不动 |
| 3 | `DROP TABLE IF EXISTS` 幂等；多实例并发由 `pg_advisory_xact_lock('adcenter_drop_partitions')` 串行化 |

**当前 dev 库不会误删**：现有最旧分区即当前月（2026-09），默认保留 13 个月时
保留窗口覆盖全部现有分区 → 任务每天跑也删不到任何东西；可放心先上线，待数据变老
（跨年后）再按需调小 `PARTITION_RETENTION_MONTHS`。

**以后加更多分区表**：只需往 `internal/store/partition.go` 的 `partitionTables`
切片追加表名（如 `budget_ledger_monthly`），预建与 TTL 清理会自动覆盖新表，
无需改定时任务逻辑。未来若要"不同表不同保留期"，再把 `partitionTables` 改为
`{table, retainMonths}` 的结构体切片即可（当前统一保留期，够用先不拆）。

---

## 10. 待办（M3+ 展开时细化）

- [ ] **M3 监控面板**：admin API 暴露队列指标 + 各广告主消耗 → 后台「系统健康」卡片
- [ ] 事件级幂等（`event_uid` 唯一键 + `ON CONFLICT DO NOTHING`，只对新插入行聚合金额）
- [ ] 死信 / 重试上限（当前失败无限重投）
- [ ] 广告主预算跨日 `daily_reset` 由聚合任务统一补记，替代实例内触发
- [ ] 秒级看板：metrics 走 Redis 分钟桶聚合（当前 DB 汇总滞后 1 分钟）
- [ ] Redis 客户端统一（预算 / 频控 / 决策缓存目前各自建 client）
