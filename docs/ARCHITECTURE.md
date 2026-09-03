# 自有广告投放管理系统 —— 技术架构设计

> 版本：V1.0（2026-09-03）
> 依据：`docs/PRD.md`（PRD V1.1）
> 部署结论：**路线 A —— 单实例常驻 + 自动重启**，P1 预留升级双实例 + Redis 路径

---

## 一、总体架构

### 1.1 技术栈与部署分工

| 组件 | 技术选型 | 部署位置 | 说明 |
|------|----------|----------|------|
| 决策 + 管理 API | Go（单二进制，单仓库） | Fly.io Singapore（常驻容器） | 进程内状态：预算扣减、频控、实时计数器 |
| 管理后台前端 | Next.js (App Router) + shadcn/ui | Vercel | 纯页面层，无常驻状态 |
| 数据库 / 认证 / 素材存储 | Postgres + Auth + Storage | Supabase SG | 与 Fly 同 region，内网延迟 <1ms |
| 素材分发 | Supabase Storage → CDN | — | 防盗链签名 URL |

### 1.2 为什么是这套组合

- **决策接口要求平均 <100ms / P99 <200ms**（PRD 非功能需求）→ Go + 进程内缓存决策路径上零 DB 查询
- **配置秒级生效** → Postgres `LISTEN/NOTIFY` 推送配置变更，Go 进程内存缓存热更新
- **监控延迟 <3s** → 实时计数器在 Go 进程内存中，通过 SSE 推给看板
- **团队小、无专职运维** → Fly.io（约 $7/月）+ Supabase 免费层 + Vercel 免费层，零运维负担

### 1.3 架构图（逻辑视图）

```
┌─────────────────┐     ┌──────────────────────────────────────────┐
│  短剧 App 客户端 │────▶│            Go 服务 (Fly.io SG)            │
│  (印尼)          │◀────│  ┌────────────┐  ┌─────────────────────┐  │
└─────────────────┘     │  │ 决策 API    │  │ 管理 API (REST)      │  │
                        │  │ /v1/ad/req  │  │ /v1/admin/*          │  │
┌─────────────────┐     │  └─────┬──────┘  └──────────┬──────────┘  │
│ 管理后台前端     │────▶│        │                     │             │
│ (Vercel)        │◀──SSE──── ───┤                     │             │
└─────────────────┘     │  ┌─────▼─────────────────────▼──────────┐  │
                        │  │            决策引擎 (engine)          │  │
                        │  │  筛选→KPI分档→优先级得分→排序→疲劳过滤  │  │
                        │  └─────┬──────────────┬────────────────┘  │
                        │        │              │                    │
                        │  ┌─────▼─────┐  ┌─────▼──────────────┐    │
                        │  │BudgetCtrl │  │ ConfigCache        │    │
                        │  │(进程内扣减)│  │ (LISTEN/NOTIFY订阅) │    │
                        │  └─────┬─────┘  └─────┬──────────────┘    │
                        │  ┌─────▼─────┐  ┌─────▼──────────────┐    │
                        │  │ FreqStore │  │ MetricsAgg (内存)   │    │
                        │  └─────┬─────┘  └─────┬──────────────┘    │
                        └────────┼──────────────┼──────────────────┘
                                 │   定时落库/持久化
                        ┌────────▼──────────────▼──────────────────┐
                        │        Supabase Postgres (SG)             │
                        │  advertisers / ad_slots / creatives /     │
                        │  decision_logs / metrics_* / budget_ledger│
                        └───────────────────────────────────────────┘
```

---

## 二、Go 服务模块设计

单仓库单二进制，包结构：

```
adcenter/
├── cmd/server/main.go          # 入口：装配所有模块
├── internal/
│   ├── api/                    # HTTP 层
│   │   ├── decision.go         # POST /v1/ad/req  广告决策（客户端调用）
│   │   ├── report.go           # POST /v1/ad/event 曝光/点击/转化上报
│   │   └── admin/              # /v1/admin/* 管理 API（供 Next.js BFF 转发）
│   ├── engine/                 # 决策引擎（纯函数，无 IO）
│   │   ├── filter.go           # 活跃筛选、定向匹配
│   │   ├── score.go            # KPI 分档 + 优先级得分（PRD 5.1）
│   │   └── select.go           # 排序、疲劳度过滤、保底份额、兜底链
│   ├── budget/                 # BudgetCtrl：进程内预算扣减 + 落库
│   ├── frequency/              # FrequencyStore：频控/间隔/疲劳度（进程内）
│   ├── config/                 # ConfigCache：LISTEN/NOTIFY 热更新
│   ├── metrics/                # MetricsAgg：实时计数器 + SSE 推送
│   ├── agent/                  # AI Agent（P1）：规则引擎起步
│   └── store/                  # Postgres 访问层（sqlc 生成）
├── web/                        # Next.js 管理后台（独立部署 Vercel）
└── docs/
```

### 2.1 决策引擎（核心路径，PRD 5.1）

请求进入后全程内存操作，**决策路径零 DB 查询**：

1. **筛选**：从 ConfigCache 取活跃广告主，按广告位 fillPriority、定向过滤
2. **KPI 分档**：`>85%` → 达优/正常保障（×1.2）；`≤85%` → 紧急保障（×0.8）
3. **得分**：`得分 = 紧急度(1/(达成率+0.01)) × 消耗进度 × 层级权重(1.5/1.2/1.0)`，消耗偏慢 ×1.3、偏快 ×0.8
4. **保底约束**：Tier1 保底 ≥30%，总保底 ≤100%，按 guaranteedShare 先切份额再排序
5. **疲劳过滤**：FreqStore 查该用户近 N 次曝光的广告主，跳过重复
6. **兜底链**：无填充 → MAX 聚合指令 → 平台自有推广
7. **记账**：BudgetCtrl 预扣（预估消耗），MetricsAgg 计数

**批量模式（count > 1，PRD 5.5）：** 得分定名单（保底 ceil 强制换入）→ 整轮下发（轮内单价降序）+ 余数给高分者 → 逐条物化（素材内选 / 频控 / 预扣，失败跳过）；只返回可用条数不补位。count 上限 20，返回条数受剩余频控额度约束。

### 2.2 BudgetCtrl（预算控制，接口隔离）

```go
type BudgetCtrl interface {
    TryDeduct(ctx, advertiserID string, amount float64) bool // 原子预扣
    Commit / Rollback                                        // 事件回执后确认
    HourlyCalibrate(ctx)                                     // 每小时平滑校准（PRD 5.2）
}
```

- 实现 A（现在）：进程内 `map[advertiserID]*atomic float` + 定时落库 `budget_ledger` 表（对账用）
- 实现 B（P1）：换 Redis Lua 脚本实现，**接口不变**，支撑双实例

### 2.3 FrequencyStore（频控）

```go
type FrequencyStore interface {
    CheckAndIncr(ctx, appID, deviceID, slotID, advertiserID string, now time.Time) bool
    // 用户身份 = (appID, deviceID)，两级策略任一不过即拒绝：
    // 广告位级（滑动24h日频控/间隔/疲劳窗口）
    // + 广告主级（多窗口滑动频控 freq_windows，多档同时生效）
}
```

- 实现 A（现在）：进程内；每 (appID, deviceID, advertiserID) 存**最近展示时间戳环形队列**（TTL = 最大窗口长度），内存估算 10 万 DAU × 人均 5 广告主 × 10 时间戳 ≈ 30MB
- 实现 B（P1）：Redis ZSET + Lua（ZREMRANGEBYSCORE 清过期 + ZCARD 计数），key 含 appID 前缀，接口不变

**两级频控体系（PRD V1.2）：** 所有时间窗均为**滑动窗口**（无自然日重置，预算的自然日重置独立）；广告主 `freq_windows` 示例：`[{180m, 3}, {1440m, 10}]`。

### 2.4 ConfigCache（配置秒级生效）

- 启动：全量加载 apps / advertisers / ad_slots / creatives 到内存（广告位按 app 分组）
- 变更：管理 API 写库后 `NOTIFY config_changed`，Go 订阅后重载对应表（<1s 生效）
- 兜底：每 60s 定时全量对账，防 NOTIFY 丢失

### 2.5 MetricsAgg（<3s 看板）

- 内存计数器（请求数/填充数/收入/eCPM，按 slot×advertiser×分钟粒度）
- SSE 端点 `/v1/admin/metrics/stream` 推送给看板
- 每分钟批量落库 `metrics_minute` 表（持久化 + 历史报表）

### 2.6 多客户端 App 隔离（V1.2 已定）

**用户身份 = (appID, deviceID)**，App 身份由服务端从 API Key 推导，客户端不可自行声明：

```
POST /v1/ad/req + X-Api-Key: adc_xxx
  → 中间件查 apps 表（sha256(key) = api_key_hash）→ 得 app_id
  → 频控 key: appID:deviceID:advertiserID（不同 App 同名 ID 天然隔离）
```

| 项 | 结论 |
|----|------|
| 广告主（预算/KPI/素材） | **全局共享**：预算跨 App 一个池，投放哪个 App 由该 App 广告位的 fill_priorities 决定 |
| 广告位 | 归属具体 App（ad_slots.app_id），填充策略独立配置 |
| 频控主体 | deviceId（开屏在登录前展示，不依赖登录态） |
| API Key | apps 表存 sha256 哈希 + 展示前缀，原文不落库；App 维度贯穿事件/指标/决策日志/预算归因 |

---

## 三、数据库设计（Supabase Postgres）

**Schema：`ads_center`**（与同实例其他项目 schema 隔离；本地开发用本机 Supabase，端口 54322）
migration 工具：golang-migrate，文件在 `migrations/`

在 PRD 第六章实体基础上细化（完整 DDL 在实现阶段以 migration 落地）：

| 表 | 说明 | 关键设计 |
|----|------|----------|
| `apps` | 客户端 App 注册表 | api_key_prefix（展示）+ api_key_hash（sha256，原文不落库）；多租户隔离的锚点 |
| `advertisers` | 广告主（全局共享） | tier、kpi 目标/实际、budget、bidding、guaranteed、targeting（jsonb）、freq_windows（jsonb 多窗口滑动频控）、end_at（投放截止，过期过滤）、priority_score |
| `ad_slots` | 广告位（归属 App） | app_id FK、type、frequency_cap（jsonb）、ai_agent（jsonb） |
| `fill_priorities` | 填充优先级（独立表） | slot_id FK、source_type、advertiser_id FK、guaranteed_share、weight、enabled、position |
| `creatives` | 素材 | advertiser_id FK、storage_path、media_type（video/image/html）、orientation（landscape/portrait/square/any）、status、weight、ab_group |
| `decision_logs` | AI 决策日志 | agent、action、old/new value、result deltas、confidence、reverted；按月分区，保留 180 天 |
| `budget_ledger` | 预算流水 | advertiser_id、预扣/确认/回滚、金额、hour_bucket（对账与平滑控制依据） |
| `metrics_minute` | 分钟级指标 | slot_id、advertiser_id、ts、requests/fills/revenue/ecpm |
| `ad_events` | 原始事件 | event_type（imp/click/conv）、device_id、可回溯审计，保留 180 天 |
| `admin_users` | 后台用户 | 用 Supabase Auth + `admin_users` 映射角色（role: super_admin/operator/analyst/strategy） |

**要点：**
- KPI 实际达成值（actualCpi）由事件聚合定时任务回写，不在决策路径上实时算
- `fill_priorities` 从 PRD 的 AdSlot 内嵌数组拆为独立表，支撑拖拽排序（position 字段）和逐条启停
- 变更审计：所有管理表配 `updated_by` + trigger 写 audit log（满足 PRD 可审计要求）

---

## 四、API 设计（摘要）

### 4.1 客户端 API（App 调用，高频）

```
POST /v1/ad/req        # 广告决策：头 X-Api-Key（服务端推导 app_id，多 App 隔离锚点）
                       # 入参 slotId、deviceId、userCtx（国家/语言等）、
                       #            count（默认 1，上限 20，批量语义见 PRD 5.5）
                       # 出参：items[]（advertiser/creative + 素材 CDN 签名 URL，
                       #       条数 ≤ count，不补位）；count=1 时可为
                       #       max_fallback / self_promo 指令
                       # 性能预算：内存路径，目标 <10ms，含网络 <100ms
POST /v1/ad/event      # 事件上报：imp/click/conv（客户端埋点 + 服务端校验），同样带 X-Api-Key
```

### 4.2 管理 API（Next.js BFF 转发，Supabase Auth JWT 鉴权 + RBAC）

```
/v1/admin/advertisers CRUD + 暂停/激活
/v1/admin/slots        CRUD + fillPriority 排序/权重/启停
/v1/admin/creatives    上传（Supabase Storage 直传 + 签名 URL）/权重/A/B
/v1/admin/agent        状态、决策日志查询、回滚、人工覆盖、策略配置
/v1/admin/metrics/stream  # SSE 实时看板
```

---

## 五、部署与可用性（路线 A）

### 5.1 部署形态

- **Fly.io Singapore 单实例**，`min_machines_running = 1`（禁用 autostop 休眠）
- 规格起步 `shared-cpu-1x 1GB`（~$7/月），CPU 或内存超 60% 持续时升配
- 崩溃自动重启（Fly health check + auto restart），恢复时间 <10s
- 成本：Fly ~$7 + Supabase 免费层 + Vercel 免费层 ≈ **$7/月**

### 5.2 故障与发版的降级策略

| 场景 | 影响 | 对策 |
|------|------|------|
| 进程崩溃 | 决策 API 中断至自动拉起（<10s） | 客户端兜底链：拿不到填充 → 直接走 MAX 聚合 → 平台兜底，**广告不空白，仅该窗口收入降级** |
| 发版部署 | 单实例滚动重启，停机窗口 10-15s | 同上降级；部署固定在**印尼凌晨低峰（北京时间凌晨 3-5 点）**，影响趋近于零 |
| Fly region 故障 | 服务不可用 | 罕见（2024-11 曾发生一次平台级故障）；接受该风险，客户端兜底链保底 |

### 5.3 P1 升级路径（路线 B，接口已预留）

`BudgetCtrl` / `FrequencyStore` 均为接口隔离，P1 换 Redis（Upstash SG，~$8/月）实现即可双实例：
- 平时 2 实例 + Fly rolling deploy → 发版零停机、单机故障无感
- 决策路径增加一次同 region Redis RTT（<1ms），性能预算仍充裕

#### 5.3.1 单实例 → 多实例迁移手册（TODO：扩容到 2 实例前必须执行）

**背景：** P0 频控/预算状态在进程内存（单副本，天然一致）；多实例下内存态必然多副本导致超发（预算双倍扣、频控双倍放行）。状态必须先外移再扩容，**顺序不能反**。

**迁移步骤（严禁跳步）：**

1. **上线前保险（P0 阶段就要做，不是迁移时做）**：`FrequencyStore` / `BudgetCtrl` 的单测写成**契约测试**（contract test）——测试套件针对接口编写（`contract_test.go`），覆盖全部边界语义（窗口边界、先裁剪后计数、跨 App 隔离、预算仅够一次时单响应最多出现一次等 ~20 用例）。P0 内存实现与 P1 Redis 实现**跑同一套用例**，Redis 实现跑不过契约测试不许上线
2. 部署 Redis（Upstash SG），**从 Postgres 回放最近 24h 事件预热**（频控 ZSET、预算从 budget_ledger 加载——预算每日消耗以 DB 为真相，本来就从 DB 加载，不会丢）
3. **保持单实例**发版切换到 Redis 实现（此步不存在内存版/Redis 版混跑；若状态预热有缺：频控最多多放几条、24h 内自愈，预算无影响）
4. 观察验证：契约测试全绿 + 线上抽样对比（同请求在两实现的判定结果）
5. 确认无误后再 `fly scale count 2` 水平扩容

**Redis 实现（P1）技术要点：**
- 频控：ZSET，key `freq:{appID}:{deviceID}:{advertiserID}`，score=时间戳；Lua 脚本原子执行 ZREMRANGEBYSCORE（清过期）→ ZCARD（窗口计数）→ 判断 → ZADD（写入）
- 预算：Lua 原子扣减；实例内存留一份粗粒度"今日已花"快照做第二道闸（Redis 不可用时收紧兜底）
- 每次调用带超时（如 50ms）；**超时/宕机 fail-open**（放行并记日志）——广告业务宁可短暂多放，不能全量无广告；事后从 Postgres 对账修正
- 回滚方案：发回内存版实现（频控 24h 自愈、预算从 DB 重载），回滚安全

**多实例数据一致性模型（为什么这么设计）：**
- 配置：每实例副本 + LISTEN/NOTIFY 广播（幂等可复制）
- 运行态（频控/预算）：Redis 唯一副本 + Lua 原子性（消灭副本，而非同步副本）
- 真相：Postgres（可从 ad_events / budget_ledger 回放重建 Redis 全部状态）

### 5.4 监控告警

- Fly 内置 CPU/内存/重启指标 + `fly logs`
- 自监控：Go 服务 `/healthz`（决策缓存/DB 连接/预算对账状态）
- 业务告警（P0 用日志 + 简单规则）：预算 ≥95% 预警、KPI <85% 预警、填充率异常下跌

---

## 六、安全设计

| 项 | 方案 |
|----|------|
| 管理后台认证 | Supabase Auth（邮箱 + 密码），JWT 透传 Go 服务校验 |
| RBAC | 四角色（super_admin / operator / analyst / strategy），Go 中间件 + Next.js 路由层双重控制 |
| 客户端 API | API Key + 设备指纹（防刷），出参素材走 CDN 签名 URL（防盗链，短有效期） |
| 传输 | 全链路 HTTPS |
| 审计 | 管理操作 + AI 决策全量日志，保留 180 天 |

### 6.1 数据访问模式：直连 Postgres（已定）

Go 后端与 migration **全部直连 Postgres 连接串**，不经过 Supabase REST/Kong 网关：

- 数据读写：`store` 包直连（连接串 + `search_path=ads_center`）
- 配置热更新：LISTEN/NOTIFY 同样走直连
- 前端仅用 Supabase 两样东西：**Auth**（`sb_publishable_` key）+ **Storage**（素材上传）
- 后台用户管理：Studio UI 手动建号（本地 127.0.0.1:54323 / 生产 Dashboard）+ SQL 插 `admin_users` 角色映射；P0 用户量为团队规模，无需程序化管理

> 本地实例（BEREAL-ADS-Backend）为共享实例（drama / mtg_agency / ads_center 多 schema 隔离），
> 且已启用新 API key 体系（`sb_publishable_` / `sb_secret_`，ES256 签名）——legacy anon/service_role JWT 不可用。

---

## 七、里程碑计划

### P0 —— MVP（核心闭环）

| 模块 | 内容 |
|------|------|
| 基建 | Go 骨架、Supabase schema migration、Fly 部署流水线、CI |
| 决策引擎 | FR 全链路：筛选→分档→得分→保底→疲劳→兜底；BudgetCtrl/FreqStore（进程内版）；ConfigCache（LISTEN/NOTIFY） |
| 广告主管理 | FR-01/02：列表、详情配置、素材上传（Supabase Storage） |
| 广告位管理 | FR-03/04：列表、策略配置（优先级排序、高级策略） |
| 监控看板 | FR-08 简化版：核心 KPI 卡片 + 广告位状态表（SSE 实时） |
| 客户端 API | /v1/ad/req、/v1/ad/event + App 联调（含兜底链） |

### P1 —— 完善（V1.0）

- AI Agent 规则引擎版（FR-05/06/07：KPI 保障 + 预算平滑自动执行、决策日志、回滚）
- 预算平滑控制全量（PRD 5.2 小时级校准自动化）
- 路线 B：Redis 外置状态 + 双实例 + 零停机发版
- 看板完善：趋势图、下钻交互、导出；告警接入

### P2 —— 增强

- AI Agent 从规则 → 模型辅助（置信度、预测）
- 广告主数量扩展优化（100+ 水平扩展验证）
- 素材自动 A/B、CTR/转化率回流优化

---

## 八、风险与待定项

| 项 | 状态 | 说明 |
|----|------|------|
| MAX 聚合联动 | 待定 | P0 由客户端本地 MAX 兜底，服务端只下发降级指令；服务端直连 MAX API 属 P2 |
| 转化归因 | 待定 | CPI/CPA 的 actualCpi 依赖客户端/第三方归因数据回传，格式需与 App 端约定 |
| 多币种 | 待定 | PRD 预算单位为美元，印尼广告主若需 IDR 展示层换算 |
| 术语表 | 已知 | PRD 术语表为转写推断，实现时以原 PDF 为准 |
