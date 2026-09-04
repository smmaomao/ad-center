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
}

export interface AdminApp {
  id: string;
  name: string;
  api_key_prefix: string;
  status: string;
}

// ============================================================
// 便捷方法
// ============================================================

export async function listAdvertisers(actorEmail: string) {
  return goApi<AdminAdvertiser[]>("/v1/admin/advertisers", actorEmail);
}

export async function listSlots(actorEmail: string) {
  return goApi<AdminSlot[]>("/v1/admin/slots", actorEmail);
}

export async function listApps(actorEmail: string) {
  return goApi<AdminApp[]>("/v1/admin/apps", actorEmail);
}
