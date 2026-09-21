"use server";

// 广告任务（Campaign）管理 Server Actions。
// 权限：页面级 requireMenu 已拦截；Go 侧对每个写接口仍复核角色。
import { redirect } from "next/navigation";
import { revalidatePath } from "next/cache";
import { getSession } from "@/lib/auth";
import {
  createCampaign,
  updateCampaign,
  deleteCampaign,
  GoApiError,
} from "@/lib/go-api";

export interface FormState {
  error?: string;
  ok?: boolean;
}

function str(fd: FormData, key: string): string {
  const v = fd.get(key);
  return typeof v === "string" ? v.trim() : "";
}

function num(fd: FormData, key: string): number {
  const v = Number.parseFloat(str(fd, key));
  return Number.isFinite(v) ? v : 0;
}

// 曝光系数：整数 1-10，默认 5（非法/缺省回落默认）
function clampConsume(v: number): number {
  if (!Number.isFinite(v) || v <= 0) return 5;
  return Math.min(10, Math.max(1, Math.round(v)));
}

/** 新建 / 编辑广告任务（隐藏 id 存在时为更新） */
export async function saveCampaignAction(
  _prev: FormState,
  fd: FormData,
): Promise<FormState> {
  const session = await getSession();
  if (!session) return { error: "会话已过期，请重新登录" };

  const name = str(fd, "name");
  const advertiserId = str(fd, "advertiser_id");
  const productId = str(fd, "product_id");
  if (!name) return { error: "任务名称必填" };
  if (!advertiserId) return { error: "必须选择所属广告主" };

  const billingMode = str(fd, "billing_mode") || "cpm";

  const fields: Record<string, unknown> = {
    advertiser_id: advertiserId,
    name,
    ...(productId ? { product_id: productId } : {}),
    status: str(fd, "status") || "active",
    bidding_price: num(fd, "bidding_price"),
    bidding_price_min: num(fd, "bidding_price_min"),
    billing_mode: billingMode,
    target_kpi_type: str(fd, "target_kpi_type") || billingMode,
    target_kpi_value: num(fd, "target_kpi_value"),
    daily_budget: num(fd, "daily_budget"),
    freq_daily_limit: Math.round(num(fd, "freq_daily_limit")) || 8,
    freq_interval_minutes: Math.round(num(fd, "freq_interval_minutes")) || 20,
    freq_fatigue_window: Math.round(num(fd, "freq_fatigue_window")) || 3,
    creative_ids: fd.getAll("creative_ids").map(String).filter(Boolean),
    consume_speed: clampConsume(num(fd, "consume_speed")),
    guaranteed_enabled: fd.get("guaranteed_enabled") === "on",
    guaranteed_min_share: Math.round(num(fd, "guaranteed_min_share")) / 100,
  };
  const startAt = str(fd, "start_at");
  const endAt = str(fd, "end_at");
  if (startAt) fields.start_at = startAt;
  if (endAt) fields.end_at = endAt;

  // 总是发送（含空串）：后端按"键是否存在"决定是否更新，空串即清空落地页地址
  fields.landing_url = str(fd, "landing_url");

  const id = str(fd, "id");
  try {
    if (id) {
      await updateCampaign(session.email, id, fields);
    } else {
      await createCampaign(session.email, fields);
    }
  } catch (e) {
    return { error: e instanceof GoApiError ? e.message : "保存失败" };
  }

  revalidatePath("/campaigns");
  if (!id) {
    // 弹窗内提交（带 no_redirect）不跳转，交由前端关闭弹窗并刷新
    if (fd.get("no_redirect")) return { ok: true };
    redirect("/campaigns");
  }
  revalidatePath(`/campaigns/${id}`);
  return { ok: true };
}

export async function deleteCampaignAction(fd: FormData): Promise<void> {
  const session = await getSession();
  if (!session) return;
  const id = str(fd, "id");
  if (!id) return;
  try {
    await deleteCampaign(session.email, id);
  } catch {
    // 列表页会重新加载，失败无副作用
  }
  revalidatePath("/campaigns");
  redirect("/campaigns");
}
