# 开发计划 —— P0 MVP

> 依据：`docs/ARCHITECTURE.md` V1.0
> 状态标记：`[ ]` 未开始 / `[~]` 进行中 / `[x]` 完成

---

## 阶段 0：项目基建

- [x] 0.1 Go 项目骨架：`cmd/server` + `internal/` 分层（api/engine/budget/frequency/config/metrics/store）
- [x] 0.2 Supabase 项目创建（SG region），schema migration 工具（golang-migrate）+ 初始 DDL（9 张表）
- [x] 0.3 Next.js 管理后台骨架（App Router + shadcn/ui），Supabase Auth 登录 + RBAC 路由守卫
- [ ] 0.4 Dockerfile + Fly.io 应用创建（Singapore，`min_machines_running=1`）+ GitHub Actions CI（test → build → deploy）
- [x] 0.5 `/healthz` 端点 + Fly health check 配置

## 阶段 1：决策引擎（后端核心）

- [x] 1.1 ConfigCache：启动全量加载 + LISTEN/NOTIFY 热更新 + 60s 对账兜底
- [x] 1.2 engine：筛选（活跃/定向/fillPriority）+ KPI 分档（×1.2 / ×0.8）
- [x] 1.3 engine：优先级得分（紧急度 × 消耗进度 × Tier 权重，±10% 速度修正）
- [x] 1.4 engine：保底份额（Tier1 ≥30%、总和 ≤100%）+ 排序填充
- [x] 1.5 FrequencyStore（进程内版）：日频控 / 间隔 / 疲劳窗口（契约测试就绪）
- [x] 1.6 BudgetCtrl（进程内版）：预扣-确认-回滚 + budget_ledger 落库（契约测试就绪）
- [x] 1.7 兜底链：MAX 降级指令 / 平台自有推广
- [x] 1.8 `POST /v1/ad/req` + `POST /v1/ad/event`（API Key 鉴权）
- [x] 1.9 决策路径基准测试（目标：引擎耗时 <10ms）

> 阶段 1 端到端验证（2026-09-04，本地 Supabase + seed 数据）：决策排序/批量/保底、
> 频控三档（间隔/疲劳/多窗口滑动 3h/3+24h/10）、SQL 改配置 NOTIFY 秒级生效、
> 事件异步落库（ad_events/metrics_minute/budget_ledger）、管理 API RBAC 全部通过。

> 基准测试（2026-09-04，Apple M5，`go test -bench`）：
> 引擎纯计算——单条×20 候选 **4.5µs**、批量20×20 候选 7.4µs、最重路径
> （批量20×200 候选+保底换入）**64µs**，对 10ms 目标余量 150 倍+；
> 频控 MemoryStore 稳态 CheckAndIncr 28-67ns（无锁竞争单协程测量）；
> HTTP 完整决策路径（鉴权+JSON 编解码+引擎+metrics+事件入队）单条 **7.7µs**、
> 批量10 12.2µs——服务端预算 8µs 级，100ms/200ms 的 SLA 中网络占绝对主导，
> 单核理论吞吐 >10 万 QPS。

## 阶段 2：管理后台（前端 + 管理 API）

- [x] 2.1 管理 API：advertisers CRUD + 暂停/激活（写库后 NOTIFY）；apps 管理（注册 App / 生成与轮换 API Key，原文仅创建时展示一次）
- [x] 2.2 管理 API：slots CRUD + fillPriorities 拖拽排序/权重/启停
- [x] 2.3 管理 API：creatives 上传（R2 presigned PUT 直传，见 ARCHITECTURE.md §2.7）/ 权重 / A/B 分组
- [x] 2.4 页面 `/advertisers`：列表、筛选、状态预警（预算将尽/KPI 未达标高亮）——接 Go API 真实数据（BFF 转发）
- [x] 2.5 页面 `/advertisers/:id` + `/new`：KPI 卡片、基本信息、KPI 与预算、投放配置、素材管理
- [x] 2.6 页面 `/slots` + `/slots/:id` + `/new`：策略配置页（优先级表格拖拽 + 高级策略表单）
- [x] 2.7 MetricsAgg：内存计数器 + 每分钟落库（SSE 端点待阶段 3）

> 阶段 2 管理 API 端到端验证（2026-09-04，本地 Supabase + BFF 会话模拟）：
> slots CRUD（创建/详情/PATCH 全量替换优先级/列表/软删；非法 source_type 与
> 保底份额 >100% 拒绝）、creatives CRUD（R2 key 格式校验防路径穿越、权重/A-B
> 分组/状态更新、软删）、BFF presign 端点（会话+角色校验、扩展名白名单、
> 100MB 上限、广告主存在性 404、未登录 API 返回 JSON 401）、决策响应含
> media_url（R2 path-style presigned GET 1h，NOTIFY 秒级生效）。
> SigV4 签名双实现（Go/TS）均通过 AWS 官方测试向量对拍；真实 R2 桶联调
> 待凭证配置后进行。

## 阶段 3：监控看板 + 联调收尾

- [x] 3.1 页面 `/dashboard`：KPI 卡片、广告主 KPI 监控表（预警标识）、广告位状态表（SSE 实时刷新）
- [x] 3.2 事件聚合定时任务：回写 填充率 / eCPM（actualCpi 回写已移除，见迁移 000052；KPI 实时达成率/自动优化推迟至下方 P1 待办）
- [ ] 3.3 App 客户端联调：决策请求、事件上报、兜底链验证（服务端不可用 → MAX）
- [ ] 3.4 业务告警（日志规则）：预算 ≥95%、KPI <85%
- [ ] 3.5 部署演练：低峰发版流程 + 崩溃自动恢复验证（kill 进程观察拉起）
- [ ] 3.6 上线 checklist：审计日志开启、180 天保留策略、素材 CDN 防盗链验证

---

## 验收标准（对照 PRD 非功能需求）

| 指标 | 要求 | 验证方式 |
|------|------|----------|
| 决策接口延迟 | 平均 <100ms / P99 <200ms | 压测（印尼模拟节点） |
| 配置生效 | 秒级 | 后台改权重 → 客户端下一请求生效 |
| 看板延迟 | <3s | SSE 推送验证 |
| 发版影响 | 客户端广告不空白 | 部署窗口期 App 实测走 MAX 兜底 |
| 进程崩溃 | <10s 恢复 | kill -9 实测 |

## P1 待办（V1.0 完善项，MVP 后启动）

- **KPI 自动优化（实时出价策略）【已推迟，本期不实现】**：基于 `target_kpi_type` / `target_kpi_value`
  （迁移 000051 已落库并可后台配置）与**运行时实测 CPI** 计算真实达成率（= target_kpi_value / 实测值），
  并据此自适应调整出价与下发紧急度（FR-06/07 自动优化）。
  - 当前状态：`actual_cpi` 已不落库（迁移 000052）；`Campaign.Achievement()` 恒返中性 1.0；
    `engine.go` 的 `urgency` 分支保持「已接入但中立」（常数 ~0.99），现有排序（价格得分 × 优先级 × 消耗节奏）不受影响。
  - 待接入：引入运行时实测值（消耗/转化按 `target_kpi_type` 口径归集）→ 在 `Achievement()` 用
    `target_kpi_value / 实测值` 计算真实达成率 → `engine.urgency` 自动恢复按达成率区分。接口已预留，无需改签名。
- **保量 / 保底份额【已推迟，本期未接入引擎、界面已隐藏】**：`campaign.guaranteed_enabled` /
  `guaranteed_min_share`（保量份额）与 `fill_priorities.guaranteed_share`（保底份额）字段已落库、
  后台可配置，但当前 `internal/engine` 决策排序填充**不读取**这两个字段做强制换入（PLAN 1.4 所标
  「保底份额 + 排序填充」在去 slot 模型重构后实际未接回引擎）；前端也已隐藏入口
  （`campaign-form` 两个字段、`campaign-view-modal`、`advertisers/[id]` 汇总卡片、`slots/priority-editor`
  保量份额列，均为 `hidden` 保留值以免编辑时清空）。
  - 待接入：① 确认保量按 campaign 级 `guaranteed_min_share` 还是 slot 级 `guaranteed_share`
    （去 slot 后后者可能应废弃）；② 在 `engine.go` 排序填充环节实现强制换入
    （PRD 514：保底条目数 ≥ ceil(占比 × count)，未入选则替换得分最低的非保底条目）。
- **指标计数 `internal/metrics` 保持进程内、不迁 Redis【已确认设计决策】**：
  它是进程内**分钟级聚合缓冲**（键 `app×style×advertiser×分钟`，计数 requests/fills/
  impressions/clicks/conversions/revenue），真相源是 Postgres `metrics_minute`
  （`FlushMinuteMetrics` 用 `ON CONFLICT ... DO UPDATE SET x = x + EXCLUDED.x` **累加 UPSERT**）。
  - 不迁理由：非跨实例需共识的状态（不像预算/频控是扣费闸），只是攒批缓冲；迁 Redis 只增
    写延迟、无正确性收益，落库仍须刷 Postgres。累加 UPSERT 本身已多实例/重试安全。
  - 多实例影响：① 落库数据**完全正确**（多实例同分钟行自动加总，不重不漏）；
    ② 实时 SSE 看板会变成「单实例视角」——推的是本实例当分钟未刷库的内存计数，
    看板连接被 fly 路由到哪台就只看到那台，进行中的分钟数偏少，刷库后完整（仅实时性瑕疵，非数据错）。
  - 当前 `min_machines_running=1`（单实例），上述影响均不存在；若未来多实例且需看板实时准确，
    可把 SSE 数据源改为直读 `metrics_minute` 的本分钟聚合，而非读单实例内存。
- AI Agent 规则引擎（FR-05/06/07）、预算平滑自动化（PRD 5.2）
- 路线 B：Redis（Upstash SG）外置 BudgetCtrl/FrequencyStore → 双实例零停机
  - **必须按 ARCHITECTURE.md §5.3.1 迁移手册执行**（先外移状态后扩容；契约测试是前置条件，P0 阶段 1.5/1.6 就要写好）
- 看板趋势图、下钻、导出；告警渠道接入
