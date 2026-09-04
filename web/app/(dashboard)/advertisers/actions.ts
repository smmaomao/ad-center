"use server";

// 广告主 + 素材的 Server Actions（PRD FR-02）。
// 权限：页面级 requireRole(super_admin/operator) 已拦截；
// Go 侧对每个写接口仍做 RBAC 复核（权威执行点）。
import { redirect } from "next/navigation";
import { revalidatePath } from "next/cache";
import { getSession } from "@/lib/auth";
import {
  createAdvertiser,
  updateAdvertiser,
  deleteAdvertiser,
  createCreative,
  updateCreative,
  deleteCreative,
  GoApiError,
} from "@/lib/go-api";

export interface FormState {
  error?: string;
}

function str(fd: FormData, key: string): string {
  const v = fd.get(key);
  return typeof v === "string" ? v.trim() : "";
}

function num(fd: FormData, key: string): number {
  const v = Number.parseFloat(str(fd, key));
  return Number.isFinite(v) ? v : 0;
}

function errMsg(e: unknown, fallback: string): string {
  if (e instanceof GoApiError) return e.message;
  return e instanceof Error ? e.message : fallback;
}

// ============================================================
// 广告主
// ============================================================

/** 新建/更新广告主（表单带 hidden id 时为更新） */
export async function saveAdvertiserAction(
  _prev: FormState,
  fd: FormData,
): Promise<FormState> {
  const session = await getSession();
  if (!session) return { error: "会话已过期，请重新登录" };

  const name = str(fd, "name");
  if (!name) return { error: "广告主名称必填" };

  const endAt = str(fd, "end_at");
  const fields: Record<string, unknown> = {
    name,
    tier: Math.max(1, Math.round(num(fd, "tier")) || 1),
    status: str(fd, "status") || "active",
    target_cpi: num(fd, "target_cpi"),
    daily_budget: num(fd, "daily_budget"),
    consume_speed: str(fd, "consume_speed") || "even",
    bidding_mode: str(fd, "bidding_mode") || "cpi",
    bidding_price: num(fd, "bidding_price"),
    guaranteed_enabled: fd.get("guaranteed_enabled") !== null,
    guaranteed_min_share: num(fd, "guaranteed_min_share"),
    contact: str(fd, "contact"),
  };
  if (endAt) fields.end_at = endAt;

  const id = str(fd, "id");
  // redirect 抛出 NEXT_REDIRECT，必须放在 try/catch 之外
  if (id) {
    try {
      await updateAdvertiser(session.email, id, fields);
    } catch (e) {
      return { error: errMsg(e, "保存失败") };
    }
    revalidatePath(`/advertisers/${id}`);
    revalidatePath("/advertisers");
    return {};
  }
  let newId: string;
  try {
    const created = await createAdvertiser(session.email, fields);
    newId = created.id;
  } catch (e) {
    return { error: errMsg(e, "创建失败") };
  }
  revalidatePath("/advertisers");
  redirect(`/advertisers/${newId}`);
}

export async function deleteAdvertiserAction(id: string): Promise<FormState> {
  const session = await getSession();
  if (!session) return { error: "会话已过期，请重新登录" };
  try {
    await deleteAdvertiser(session.email, id);
  } catch (e) {
    return { error: errMsg(e, "删除失败") };
  }
  revalidatePath("/advertisers");
  redirect("/advertisers");
}

// ============================================================
// 素材（R2 直传完成后注册元数据；html 为外链）
// ============================================================

export interface RegisterCreativeInput {
  advertiser_id: string;
  name: string;
  media_type: "video" | "image" | "html";
  storage_path: string; // video/image: object_key；html: URL
  file_size_bytes: number;
  orientation?: string;
  width?: number;
  height?: number;
  duration_ms?: number;
}

export async function registerCreativeAction(
  input: RegisterCreativeInput,
): Promise<FormState> {
  const session = await getSession();
  if (!session) return { error: "会话已过期，请重新登录" };
  try {
    await createCreative(session.email, { ...input, status: "active" });
  } catch (e) {
    return { error: errMsg(e, "素材注册失败") };
  }
  revalidatePath(`/advertisers/${input.advertiser_id}`);
  return {};
}

export async function updateCreativeAction(
  id: string,
  advertiserId: string,
  fields: { name?: string; status?: string; weight?: number; ab_group?: string },
): Promise<FormState> {
  const session = await getSession();
  if (!session) return { error: "会话已过期，请重新登录" };
  try {
    await updateCreative(session.email, id, fields);
  } catch (e) {
    return { error: errMsg(e, "素材更新失败") };
  }
  revalidatePath(`/advertisers/${advertiserId}`);
  return {};
}

export async function deleteCreativeAction(
  id: string,
  advertiserId: string,
): Promise<FormState> {
  const session = await getSession();
  if (!session) return { error: "会话已过期，请重新登录" };
  try {
    await deleteCreative(session.email, id);
  } catch (e) {
    return { error: errMsg(e, "素材删除失败") };
  }
  revalidatePath(`/advertisers/${advertiserId}`);
  return {};
}
