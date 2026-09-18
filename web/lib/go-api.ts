// Go 管理 API 客户端（BFF 转发，ARCHITECTURE.md §4.2）：
// 浏览器不直连 Go。Next.js 服务端持会话，以内部密钥 + 操作者邮箱调用，
// RBAC 权威执行点在 Go 侧（requireRole 复核 admin_users 角色）。
export const GO_API_URL = process.env.GO_API_URL ?? "http://127.0.0.1:8888";
export const INTERNAL_API_KEY = process.env.INTERNAL_API_KEY ?? "";

export class GoApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message);
    this.name = "GoApiError";
  }
}

/** 服务端转发请求：注入内部密钥与操作者（审计 + RBAC） */
export async function goApi<T>(
  path: string,
  actorEmail: string,
  init?: RequestInit,
): Promise<T> {
  if (!INTERNAL_API_KEY) {
    throw new GoApiError(500, "INTERNAL_API_KEY not configured");
  }
  const res = await fetch(GO_API_URL + path, {
    ...init,
    headers: {
      "Content-Type": "application/json",
      "X-Internal-Key": INTERNAL_API_KEY,
      "X-Actor-Email": actorEmail,
      ...init?.headers,
    },
    // 管理 API 期望实时数据（写后立刻读），不做 fetch 缓存
    cache: "no-store",
  });
  const body = (await res.json().catch(() => ({}))) as T & { error?: string };
  if (!res.ok) {
    throw new GoApiError(res.status, body.error ?? res.statusText);
  }
  return body;
}

// ============================================================
// 管理 API 类型（与 Go internal/store 对应）
// ============================================================

export interface AdminAdvertiser {
  id: string;
  name: string;
  status: "active" | "paused" | "budget_exhausted";
  // 钱包：总余额是投放硬顶（累计充值 - 累计扣费），首次充值自动启用
  wallet_enabled: boolean;
  wallet_balance: number;
  contact: string;
  notes: string;
  created_at: string | null;
}

/**
 * CPA 转化事件（= 归因方 S2S 回调 event_name，对应 Go config.ConversionEvents）。
 * 计费方式为 cpa 时，按下列事件分别配置单价区间 [min,max]（美元）。
 */
export const CPA_EVENTS = [
  { key: "install", label: "安装" },
  { key: "activate", label: "激活" },
  { key: "register", label: "注册" },
  { key: "first_purchase", label: "首充" },
  { key: "purchase", label: "充值" },
] as const;

export interface AdminSlot {
  id: string;
  app_id: string;
  app_name: string;
  key: string;
  name: string;
  type: string;
  status: string;
  freq_daily_limit: number;
  freq_interval_minutes: number;
  freq_fatigue_window: number;
  fill_count: number;
  ai_agent_enabled: boolean;
  ai_agent_goal: string;
}

export interface AdminApp {
  id: string;
  name: string;
  status: string;
  callback_url: string | null; // 业务后端 S2S 接收地址
  secret_key: string; // S2S 签名密钥（可重置）
  api_key: string | null; // API Key 明文（仅展示/复制，鉴权走 hash）
  created_at?: string | null; // 创建日期（YYYY-MM-DD）
}

/** 更新 App（名称 / 状态 / 业务回调地址） */
export async function updateApp(
  actorEmail: string,
  id: string,
  body: { name?: string; status?: string; callback_url?: string },
) {
  return goSend<{ status: string }>(
    `/v1/admin/apps/${id}`,
    actorEmail,
    "PATCH",
    body,
  );
}

/** 重置 S2S 签名密钥（旧密钥立即失效） */
export async function resetAppSecret(actorEmail: string, id: string) {
  return goSend<{ secret: string }>(
    `/v1/admin/apps/${id}/secret`,
    actorEmail,
    "POST",
  );
}

/** 重置 API Key（旧密钥立即失效，返回新密钥） */
export async function resetAppKey(actorEmail: string, id: string) {
  return goSend<{ key: string }>(
    `/v1/admin/apps/${id}/key`,
    actorEmail,
    "POST",
  );
}

export interface AdminCreative {
  id: string;
  advertiser_id: string;
  name: string;
  media_type: "video" | "image" | "html";
  storage_path: string;
  file_size_bytes: number;
  orientation: string;
  width: number;
  height: number;
  duration_ms: number;
  status: "active" | "testing" | "paused";
  weight: number; // 优先级系数 0.1~5.0
  ab_group: string;
  // 展现样式（多选）：splash / rewarded_video / interstitial / feed / banner
  styles: string[];
  // 投放目标 App（多选，空 = 全部 App）
  target_apps: string[];
  // 商业与策略（跟着素材走）
  billing_mode: "cpm" | "cpc" | "cpa";
  price: number;
  cpa_event: string;
  start_at: string | null;
  end_at: string | null;
}

/** 展现样式枚举（与后端 conversion/样式一致，5 个） */
export const CREATIVE_STYLES = [
  { key: "splash", label: "开屏" },
  { key: "rewarded_video", label: "激励视频" },
  { key: "interstitial", label: "插屏" },
  { key: "feed", label: "信息流" },
  { key: "banner", label: "Banner" },
] as const;

// CPA 计费事件复用上方的 CPA_EVENTS（安装 / 激活 / 注册 / 首充 / 充值）

export interface AdminPriority {
  id: string;
  source_type: "advertiser" | "max" | "fallback";
  advertiser_id?: string;
  advertiser_name?: string;
  expected_ecpm: number;
  guaranteed_share: number;
  weight: number;
  enabled: boolean;
  position: number;
}

export interface AdminSlotDetail extends AdminSlot {
  priorities: AdminPriority[];
}

/** 填充优先级写入（保存时全量替换，position = 数组下标） */
export interface PriorityInput {
  source_type: "advertiser" | "max" | "fallback";
  advertiser_id?: string;
  expected_ecpm?: number;
  guaranteed_share?: number;
  weight?: number;
  enabled: boolean;
}

// ============================================================
// 便捷方法
// ============================================================

async function goSend<T>(
  path: string,
  actorEmail: string,
  method: string,
  body?: unknown,
): Promise<T> {
  return goApi<T>(path, actorEmail, {
    method,
    body: body === undefined ? undefined : JSON.stringify(body),
  });
}

export async function listAdvertisers(actorEmail: string) {
  return goApi<AdminAdvertiser[]>("/v1/admin/advertisers", actorEmail);
}

export async function getAdvertiser(actorEmail: string, id: string) {
  return goApi<AdminAdvertiser>(`/v1/admin/advertisers/${id}`, actorEmail);
}

/** 新建/更新广告主（字段白名单在 Go 侧复核） */
export async function createAdvertiser(
  actorEmail: string,
  fields: Record<string, unknown>,
) {
  return goSend<{ id: string }>("/v1/admin/advertisers", actorEmail, "POST", fields);
}

export async function updateAdvertiser(
  actorEmail: string,
  id: string,
  fields: Record<string, unknown>,
) {
  return goSend<{ status: string }>(`/v1/admin/advertisers/${id}`, actorEmail, "PATCH", fields);
}

export async function deleteAdvertiser(actorEmail: string, id: string) {
  return goSend<{ status: string }>(`/v1/admin/advertisers/${id}`, actorEmail, "DELETE");
}

// ============================================================
// 广告主总钱包（充值 - 扣费；余额为投放硬顶）
// ============================================================

/** 钱包概览（GET /v1/admin/advertisers/{id}/wallet） */
export interface AdminWallet {
  advertiser_id: string;
  /** 是否已启用总钱包闸：false=存量广告主，不受总余额限制；首次充值后为 true */
  enabled: boolean;
  balance: number; // 当前余额
  deposited: number; // 累计充值
  spent: number; // 累计扣费
  currency: string;
}

/** 钱包流水一行（充值 + 调账 + 扣费合并，时间倒序） */
export interface WalletFlowRow {
  kind: "recharge" | "deduct" | "adjust";
  amount: number; // recharge/deduct 恒为正（方向由 kind 区分）；adjust 为带符号变动额
  currency: string;
  op_type?: string; // 扣费细分：deduct / deduct_agg
  note?: string;
  created_by?: string;
  at: string; // YYYY-MM-DD HH24:MI:SS
}

export async function getWallet(actorEmail: string, id: string) {
  return goApi<AdminWallet>(`/v1/admin/advertisers/${id}/wallet`, actorEmail);
}

export async function listWalletFlow(
  actorEmail: string,
  id: string,
  limit = 200,
) {
  return goApi<WalletFlowRow[]>(
    `/v1/admin/advertisers/${id}/wallet/flow?limit=${limit}`,
    actorEmail,
  );
}

/** 充值：写充值流水 + 递增余额 + 启用钱包闸 */
export async function rechargeWallet(
  actorEmail: string,
  id: string,
  body: { amount: number; currency?: string; note?: string },
) {
  return goSend<{ status: string; balance: number }>(
    `/v1/admin/advertisers/${id}/wallet/recharge`,
    actorEmail,
    "POST",
    body,
  );
}

/**
 * 手动调账：修正余额 + 写调账流水（不改钱包闸开关）。
 * - mode="set"：把余额改为 amount
 * - mode="delta"：在现有余额上增减 amount（可正可负）
 */
export async function adjustWallet(
  actorEmail: string,
  id: string,
  body: { mode: "set" | "delta"; amount: number; note?: string },
) {
  return goSend<{ status: string; balance: number }>(
    `/v1/admin/advertisers/${id}/wallet/adjust`,
    actorEmail,
    "POST",
    body,
  );
}

// ============================================================
// 运行时设置（决策缓存等）
// ============================================================

export interface DecisionCacheConfig {
  enabled: boolean;
  ttl_seconds: number;
}

/** 读取决策缓存配置（GET /v1/admin/settings/decision_cache） */
export async function getDecisionCache(
  actorEmail: string,
): Promise<DecisionCacheConfig> {
  const r = await goApi<{ value: DecisionCacheConfig }>(
    "/v1/admin/settings/decision_cache",
    actorEmail,
  );
  return r.value;
}

/** 更新决策缓存配置（PATCH /v1/admin/settings/decision_cache） */
export async function updateDecisionCache(
  actorEmail: string,
  cfg: DecisionCacheConfig,
): Promise<void> {
  await goSend<{ status: string }>(
    "/v1/admin/settings/decision_cache",
    actorEmail,
    "PATCH",
    { value: cfg },
  );
}

// ============================================================
// 平台计费标准线（出价基准分的分母，系统设置项）
// 素材出价基准分 = 素材实际出价 ÷ 对应扣费模式的标准线 × 100
// ============================================================

export interface PricingBenchmark {
  cpm: number;
  cpc: number;
  cpa_install: number;
  cpa_activate: number;
  cpa_register: number;
  cpa_first_purchase: number;
}

/** 后台从未配置时的兜底值（与 Go config.DefaultPricingBenchmark 保持一致） */
export const DEFAULT_PRICING_BENCHMARK: PricingBenchmark = {
  cpm: 15,
  cpc: 5,
  cpa_install: 10,
  cpa_activate: 12,
  cpa_register: 15,
  cpa_first_purchase: 20,
};

/** 读取平台计费标准线（未配置时接口返回 404，调用方需 catch 用兜底值） */
export async function getPricingBenchmark(
  actorEmail: string,
): Promise<PricingBenchmark> {
  const r = await goApi<{ value: PricingBenchmark }>(
    "/v1/admin/settings/pricing_benchmark",
    actorEmail,
  );
  return r.value;
}

export async function updatePricingBenchmark(
  actorEmail: string,
  b: PricingBenchmark,
): Promise<void> {
  await goSend<{ status: string }>(
    "/v1/admin/settings/pricing_benchmark",
    actorEmail,
    "PATCH",
    { value: b },
  );
}

// ============================================================
// 用户疲劳度（全局频控）配置（系统设置 → 全局频控配置）
// 控制1：window_minutes 分钟内同一用户看同一素材最多 window_max 次；
// 控制2：每日（滚动24h）同一用户看同一素材最多 daily_max 次。
// ============================================================

export async function listSlots(actorEmail: string) {
  return goApi<AdminSlot[]>("/v1/admin/slots", actorEmail);
}

export async function getSlot(actorEmail: string, id: string) {
  return goApi<AdminSlotDetail>(`/v1/admin/slots/${id}`, actorEmail);
}

export interface SlotPayload {
  app_id?: string;
  slot_key?: string;
  name?: string;
  type?: string;
  status?: string;
  freq_daily_limit?: number;
  freq_interval_minutes?: number;
  freq_fatigue_window?: number;
  ai_agent_enabled?: boolean;
  ai_agent_goal?: string;
  priorities?: PriorityInput[];
}

export async function createSlot(actorEmail: string, body: SlotPayload) {
  return goSend<{ id: string }>("/v1/admin/slots", actorEmail, "POST", body);
}

export async function updateSlot(actorEmail: string, id: string, body: SlotPayload) {
  return goSend<{ status: string }>(`/v1/admin/slots/${id}`, actorEmail, "PATCH", body);
}

export async function deleteSlot(actorEmail: string, id: string) {
  return goSend<{ status: string }>(`/v1/admin/slots/${id}`, actorEmail, "DELETE");
}

/** 软删 App（保留历史归因与事件数据） */
export async function deleteApp(actorEmail: string, id: string) {
  return goSend<{ status: string }>(`/v1/admin/apps/${id}`, actorEmail, "DELETE");
}

export async function listApps(actorEmail: string) {
  return goApi<AdminApp[]>("/v1/admin/apps", actorEmail);
}

/** 注册 App：返回 Ad_App_Id 与 API Key（Key 仅此一次返回，之后不可再取） */
export async function createApp(actorEmail: string, name: string) {
  return goSend<{ id: string; api_key: string }>(
    "/v1/admin/apps",
    actorEmail,
    "POST",
    { name },
  );
}

export async function listCreatives(actorEmail: string, advertiserId?: string) {
  const q = advertiserId ? `?advertiser_id=${encodeURIComponent(advertiserId)}` : "";
  return goApi<AdminCreative[]>(`/v1/admin/creatives${q}`, actorEmail);
}

/** 注册素材元数据（video/image 为 R2 直传后的 object_key，html 为外链 URL） */
export async function createCreative(
  actorEmail: string,
  fields: Record<string, unknown>,
) {
  return goSend<{ id: string }>("/v1/admin/creatives", actorEmail, "POST", fields);
}

export async function updateCreative(
  actorEmail: string,
  id: string,
  fields: Record<string, unknown>,
) {
  return goSend<{ status: string }>(`/v1/admin/creatives/${id}`, actorEmail, "PATCH", fields);
}

export async function deleteCreative(actorEmail: string, id: string) {
  return goSend<{ status: string }>(`/v1/admin/creatives/${id}`, actorEmail, "DELETE");
}

/** 后台预览：把素材 storage_path 签成可播放地址（video/image 走 R2 预签名，html 直链） */
export async function getCreativePlayUrl(actorEmail: string, id: string) {
  return goApi<{ url: string; media_type: string }>(
    `/v1/admin/creatives/${id}/play-url`,
    actorEmail,
  );
}

// ============================================================
// 广告任务（Campaign）：广告主下的出价 / KPI / 单价 / 频控 + 关联素材
// ============================================================

// 计费方式 / KPI 指标类型共用枚举（出价方式 = 唯一扣费锚点；KPI 类型通常与其一致）
export type BillingMode =
  | "cpm"
  | "cpc"
  | "cpi"
  | "cpa-activate"
  | "cpa-register"
  | "cpa-first-deposit"
  | "cpa-pay";

export interface AdminCampaign {
  id: string;
  advertiser_id: string;
  advertiser_name: string;
  name: string;
  status: "active" | "paused";
  // 出价 = 计费方式维度的单价，且是区间：不填 min = 固定单价；填了则在 [min,max] 随机
  bidding_price: number; // 上限
  bidding_price_min: number; // 下限（0 = 不启用区间）
  // 计费方式（唯一扣费锚点）：决定按哪个事件扣费 + 出价即该事件单价
  billing_mode: BillingMode;
  cpa_event_prices?: Record<string, [number, number]> | null;
  // 目标 KPI：类型通常 = 计费方式；值即目标（如目标 CPI=$1.80）
  target_kpi_type: BillingMode;
  target_kpi_value: number;
  daily_budget: number;
  spent_today: number;
  // 曝光系数 1-10（默认 5）：影响下发节奏
  consume_speed: number;
  guaranteed_enabled: boolean;
  guaranteed_min_share: number;
  priority_score: number;
  freq_daily_limit: number;
  freq_interval_minutes: number;
  freq_fatigue_window: number;
  creative_ids: string[];
  product_id?: string;
  product_name?: string;
  start_at?: string | null;
  end_at?: string | null;
  // 下发有效期（分钟，0/未设=兜底 10）；从广告主下沉到本任务维度（migration 000035）
  deliver_ttl_minutes?: number;
  // 落地页地址：用户点击后服务端据此生成跳转地址（migration 000039）
  landing_url?: string;
}

/** 按广告主 / 产品归集的 KPI 汇总（campaign 为执行粒度，product/advertiser 是归集层） */
export interface CampaignRollup {
  campaign_count: number;
  daily_budget: number;
  spent_today: number;
  target_kpi_type: BillingMode;
  target_kpi_value: number;
  achievement: number;
  guaranteed_min_share: number;
}

/** 广告主维度 KPI 汇总（旗下所有 campaign） */
export async function getAdvertiserRollup(actorEmail: string, id: string) {
  return goApi<CampaignRollup>(
    `/v1/admin/advertisers/${id}/rollup`,
    actorEmail,
  );
}

/** 产品维度 KPI 汇总（该产品下所有 campaign） */
export async function getProductRollup(actorEmail: string, id: string) {
  return goApi<CampaignRollup>(`/v1/admin/products/${id}/rollup`, actorEmail);
}

/** 产品（Product）：广告主下的产品归集，一个广告主可有多款产品各自跑 campaign */
export interface AdminProduct {
  id: string;
  advertiser_id: string;
  advertiser_name: string;
  name: string;
  status: "active" | "paused";
  daily_budget: number;
  notes: string;
  created_at: string | null;
  updated_at: string | null;
}

export async function listCampaigns(actorEmail: string) {
  return goApi<AdminCampaign[]>("/v1/admin/campaigns", actorEmail);
}

export async function getCampaign(actorEmail: string, id: string) {
  return goApi<AdminCampaign>(`/v1/admin/campaigns/${id}`, actorEmail);
}

export async function createCampaign(
  actorEmail: string,
  fields: Record<string, unknown>,
) {
  return goSend<{ id: string }>("/v1/admin/campaigns", actorEmail, "POST", fields);
}

export async function updateCampaign(
  actorEmail: string,
  id: string,
  fields: Record<string, unknown>,
) {
  return goSend<{ status: string }>(
    `/v1/admin/campaigns/${id}`,
    actorEmail,
    "PATCH",
    fields,
  );
}

export async function deleteCampaign(actorEmail: string, id: string) {
  return goSend<{ status: string }>(
    `/v1/admin/campaigns/${id}`,
    actorEmail,
    "DELETE",
  );
}

// ============================================================
// 产品（Product）：广告主下的产品归集（多产品广告主）
// ============================================================

export async function listProducts(actorEmail: string, advertiserId?: string) {
  const q = advertiserId
    ? `?advertiser_id=${encodeURIComponent(advertiserId)}`
    : "";
  return goApi<AdminProduct[]>(`/v1/admin/products${q}`, actorEmail);
}

export async function getProduct(actorEmail: string, id: string) {
  return goApi<AdminProduct>(`/v1/admin/products/${id}`, actorEmail);
}

export async function createProduct(
  actorEmail: string,
  fields: Record<string, unknown>,
) {
  return goSend<{ id: string }>("/v1/admin/products", actorEmail, "POST", fields);
}

export async function updateProduct(
  actorEmail: string,
  id: string,
  fields: Record<string, unknown>,
) {
  return goSend<{ status: string }>(
    `/v1/admin/products/${id}`,
    actorEmail,
    "PATCH",
    fields,
  );
}

export async function deleteProduct(actorEmail: string, id: string) {
  return goSend<{ status: string }>(
    `/v1/admin/products/${id}`,
    actorEmail,
    "DELETE",
  );
}

// ============================================================
// 后台 RBAC：用户 / 角色 / 菜单（migration 000015）
// 全部接口仅 super_admin（Go 侧 requireRole needSuper）
// ============================================================

export interface AdminUserItem {
  auth_user_id: string;
  email: string;
  role: string;
  status: "active" | "disabled";
  created_at: string | null;
}

export interface AdminRoleItem {
  code: string;
  name: string;
  description: string;
  is_system: boolean;
  menu_count: number;
  created_at: string | null;
}

export interface AdminMenuItem {
  code: string;
  label: string;
  href: string | null;
  parent_code: string | null;
  sort_order: number;
  enabled: boolean;
}

export const listUsers = (actorEmail: string) =>
  goApi<AdminUserItem[]>("/v1/admin/users", actorEmail);

export const createUser = (
  actorEmail: string,
  body: { email: string; role: string; status?: string; password?: string },
) => goSend<{ id: string }>("/v1/admin/users", actorEmail, "POST", body);

export const updateUser = (
  actorEmail: string,
  id: string,
  body: { email?: string; role?: string; status?: string; password?: string },
) =>
  goSend<{ status: string }>(`/v1/admin/users/${id}`, actorEmail, "PATCH", body);

export const deleteUser = (actorEmail: string, id: string) =>
  goSend<{ status: string }>(`/v1/admin/users/${id}`, actorEmail, "DELETE");

export const listRoles = (actorEmail: string) =>
  goApi<AdminRoleItem[]>("/v1/admin/roles", actorEmail);

export const createRole = (
  actorEmail: string,
  body: { code: string; name: string; description?: string },
) => goSend<{ id: string }>("/v1/admin/roles", actorEmail, "POST", body);

export const updateRole = (
  actorEmail: string,
  code: string,
  body: { name?: string; description?: string },
) =>
  goSend<{ status: string }>(`/v1/admin/roles/${code}`, actorEmail, "PATCH", body);

export const deleteRole = (actorEmail: string, code: string) =>
  goSend<{ status: string }>(`/v1/admin/roles/${code}`, actorEmail, "DELETE");

/** 角色已授权的菜单 code（勾选回显） */
export const getRoleMenus = (actorEmail: string, code: string) =>
  goApi<{ menu_codes: string[] }>(`/v1/admin/roles/${code}/menus`, actorEmail);

/** 全量替换角色授权 */
export const setRoleMenus = (
  actorEmail: string,
  code: string,
  menuCodes: string[],
) =>
  goSend<{ status: string }>(`/v1/admin/roles/${code}/menus`, actorEmail, "PUT", {
    menu_codes: menuCodes,
  });

export const listMenus = (actorEmail: string) =>
  goApi<AdminMenuItem[]>("/v1/admin/menus", actorEmail);

export const createMenu = (
  actorEmail: string,
  body: {
    code: string;
    label: string;
    href?: string;
    parent_code?: string;
    sort_order?: number;
    enabled?: boolean;
  },
) => goSend<{ id: string }>("/v1/admin/menus", actorEmail, "POST", body);

export const updateMenu = (
  actorEmail: string,
  code: string,
  body: {
    label?: string;
    href?: string;
    parent_code?: string;
    sort_order?: number;
    enabled?: boolean;
  },
) => goSend<{ status: string }>(`/v1/admin/menus/${code}`, actorEmail, "PATCH", body);

export const deleteMenu = (actorEmail: string, code: string) =>
  goSend<{ status: string }>(`/v1/admin/menus/${code}`, actorEmail, "DELETE");
