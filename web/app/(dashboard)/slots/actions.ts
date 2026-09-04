"use server";

// 广告位 + 填充优先级的 Server Actions（PRD FR-03/FR-04）。
// 优先级保存为全量替换（拖拽排序语义），事务在 Go 侧完成。
import { redirect } from "next/navigation";
import { revalidatePath } from "next/cache";
import { getSession } from "@/lib/auth";
import {
  createSlot,
  updateSlot,
  deleteSlot,
  GoApiError,
  type PriorityInput,
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

const SLOT_KEY_RE = /^[a-z0-9_]{2,64}$/;

export async function createSlotAction(
  _prev: FormState,
  fd: FormData,
): Promise<FormState> {
  const session = await getSession();
  if (!session) return { error: "会话已过期，请重新登录" };

  const name = str(fd, "name");
  const appId = str(fd, "app_id");
  const slotKey = str(fd, "slot_key");
  if (!name || !appId || !slotKey) {
    return { error: "所属 App、slot_key、名称必填" };
  }
  if (!SLOT_KEY_RE.test(slotKey)) {
    return { error: "slot_key 仅允许小写字母/数字/下划线，2-64 位（客户端稳定标识，创建后不可改）" };
  }

  let newId: string;
  try {
    const created = await createSlot(session.email, {
      app_id: appId,
      slot_key: slotKey,
      name,
      type: str(fd, "type") || "rewarded_video",
      status: str(fd, "status") || "active",
      freq_daily_limit: num(fd, "freq_daily_limit"),
      freq_interval_minutes: num(fd, "freq_interval_minutes"),
      freq_fatigue_window: num(fd, "freq_fatigue_window"),
      ai_agent_enabled: fd.get("ai_agent_enabled") !== null,
      ai_agent_goal: str(fd, "ai_agent_goal"),
    });
    newId = created.id;
  } catch (e) {
    return { error: errMsg(e, "创建失败") };
  }
  revalidatePath("/slots");
  redirect(`/slots/${newId}`);
}

export async function updateSlotAction(
  _prev: FormState,
  fd: FormData,
): Promise<FormState> {
  const session = await getSession();
  if (!session) return { error: "会话已过期，请重新登录" };
  const id = str(fd, "id");
  if (!id) return { error: "缺少广告位 ID" };

  try {
    // ai_agent_enabled 为 bool 语义，PATCH 总是显式写入当前勾选状态
    await updateSlot(session.email, id, {
      name: str(fd, "name"),
      type: str(fd, "type"),
      status: str(fd, "status"),
      freq_daily_limit: num(fd, "freq_daily_limit"),
      freq_interval_minutes: num(fd, "freq_interval_minutes"),
      freq_fatigue_window: num(fd, "freq_fatigue_window"),
      ai_agent_enabled: fd.get("ai_agent_enabled") !== null,
      ai_agent_goal: str(fd, "ai_agent_goal"),
    });
  } catch (e) {
    return { error: errMsg(e, "保存失败") };
  }
  revalidatePath(`/slots/${id}`);
  revalidatePath("/slots");
  return {};
}

/** 保存填充优先级：全量替换，position = 数组下标（拖拽排序语义） */
export async function savePrioritiesAction(
  slotId: string,
  priorities: PriorityInput[],
): Promise<FormState> {
  const session = await getSession();
  if (!session) return { error: "会话已过期，请重新登录" };
  const sum = priorities.reduce((acc, p) => acc + (p.guaranteed_share ?? 0), 0);
  if (sum > 1.0001) {
    return { error: `保量份额总和 ${sum.toFixed(2)} 超过 1` };
  }
  try {
    await updateSlot(session.email, slotId, { priorities });
  } catch (e) {
    return { error: errMsg(e, "保存优先级失败") };
  }
  revalidatePath(`/slots/${slotId}`);
  return {};
}

export async function deleteSlotAction(id: string): Promise<FormState> {
  const session = await getSession();
  if (!session) return { error: "会话已过期，请重新登录" };
  try {
    await deleteSlot(session.email, id);
  } catch (e) {
    return { error: errMsg(e, "删除失败") };
  }
  revalidatePath("/slots");
  redirect("/slots");
}
