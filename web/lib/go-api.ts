// Go 管理 API 客户端（BFF 转发，ARCHITECTURE.md §4.2）：
// 浏览器不直连 Go。Next.js 服务端持会话，以内部密钥 + 操作者邮箱调用，
// RBAC 权威执行点在 Go 侧（requireRole 复核 admin_users 角色）。
const GO_API_URL = process.env.GO_API_URL ?? "http://127.0.0.1:18080";
const INTERNAL_API_KEY = process.env.INTERNAL_API_KEY ?? "";

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
  tier: number;
  status: "active" | "paused" | "budget_exhausted";
  target_cpi: number;
  actual_cpi: number;
  achievement: number;
  daily_budget: number;
  spent_today: number;
  consume_speed: "even" | "accelerated" | "asap";
  bidding_mode: "cpi" | "cpa" | "revenue_share";
  bidding_price: number;
  guaranteed_enabled: boolean;
  guaranteed_min_share: number;
  end_at: string | null;
  contact: string;
}

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
  api_key_prefix: string;
  status: string;
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
  weight: number;
  ab_group: string;
}

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

export async function listApps(actorEmail: string) {
  return goApi<AdminApp[]>("/v1/admin/apps", actorEmail);
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
