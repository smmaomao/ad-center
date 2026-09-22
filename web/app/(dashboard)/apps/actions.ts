"use server";

import { revalidatePath } from "next/cache";
import { getSession } from "@/lib/auth";
import { updateApp, resetAppSecret, resetAppKey, createApp, deleteApp } from "@/lib/go-api";

function str(fd: FormData, key: string): string {
  const v = fd.get(key);
  return typeof v === "string" ? v.trim() : "";
}

export interface CreateResult {
  id?: string;
  api_key?: string;
  error?: string;
}

/** 注册新 App：API Key 只在创建时返回一次（通过弹窗一次性展示） */
export async function createAppAction(
  _prev: CreateResult,
  fd: FormData,
): Promise<CreateResult> {
  const session = await getSession();
  if (!session) return { error: "未登录" };
  const name = str(fd, "name");
  if (!name) return { error: "请填写应用名称" };
  const adWatchParams = str(fd, "ad_watch_params");
  if (adWatchParams) {
    try {
      JSON.parse(adWatchParams);
    } catch {
      return { error: "ad_watch_params 必须是合法 JSON 对象" };
    }
  }
  const addServerID = str(fd, "add_server_id");
  try {
    const r = await createApp(session.email, name, adWatchParams, addServerID);
    revalidatePath("/apps");
    return { id: r.id, api_key: r.api_key };
  } catch (e) {
    return { error: e instanceof Error ? e.message : "创建失败" };
  }
}

export interface UpdateResult {
  ok?: boolean;
  error?: string;
}

/** 更新应用名称 / 状态 / 业务回调地址 */
export async function updateAppAction(
  _prev: UpdateResult,
  fd: FormData,
): Promise<UpdateResult> {
  const session = await getSession();
  if (!session) return { error: "未登录" };
  const id = str(fd, "id");
  if (!id) return { error: "缺少 id" };

  const fields: Record<string, string> = {};
  const name = str(fd, "name");
  if (name) fields.name = name;
  const status = str(fd, "status");
  if (status) fields.status = status;
  // 回调地址允许清空
  fields.callback_url = str(fd, "callback_url");

  // 完播回传附加参数（JSON 对象），允许清空
  const adWatchParams = str(fd, "ad_watch_params");
  if (adWatchParams) {
    try {
      JSON.parse(adWatchParams);
    } catch {
      return { error: "ad_watch_params 必须是合法 JSON 对象" };
    }
  }
  fields.ad_watch_params = adWatchParams;

  // app 服务端侧 id（转发 reward 回调时作为 app_id；留空则回退 app_code）
  fields.add_server_id = str(fd, "add_server_id");

  try {
    await updateApp(session.email, id, fields);
    revalidatePath("/apps");
    return { ok: true };
  } catch (e) {
    return { error: e instanceof Error ? e.message : "保存失败" };
  }
}

export interface ResetResult {
  secret?: string;
  error?: string;
}

/** 重置 S2S 签名密钥（旧密钥立即失效） */
export async function resetAppSecretAction(
  _prev: ResetResult,
  fd: FormData,
): Promise<ResetResult> {
  const session = await getSession();
  if (!session) return { error: "未登录" };
  const id = str(fd, "id");
  if (!id) return { error: "缺少 id" };
  try {
    const r = await resetAppSecret(session.email, id);
    revalidatePath("/apps");
    return { secret: r.secret };
  } catch (e) {
    return { error: e instanceof Error ? e.message : "重置失败" };
  }
}

export interface ResetKeyResult {
  key?: string;
  error?: string;
}

/** 重置 API Key（旧密钥立即失效，返回新密钥） */
export async function resetAppKeyAction(
  _prev: ResetKeyResult,
  fd: FormData,
): Promise<ResetKeyResult> {
  const session = await getSession();
  if (!session) return { error: "未登录" };
  const id = str(fd, "id");
  if (!id) return { error: "缺少 id" };
  try {
    const r = await resetAppKey(session.email, id);
    revalidatePath("/apps");
    return { key: r.key };
  } catch (e) {
    return { error: e instanceof Error ? e.message : "重置失败" };
  }
}

export interface DeleteResult {
  ok?: boolean;
  error?: string;
}

/** 软删 App（保留历史归因与事件数据） */
export async function deleteAppAction(
  id: string,
): Promise<DeleteResult> {
  const session = await getSession();
  if (!session) return { error: "未登录" };
  try {
    await deleteApp(session.email, id);
    revalidatePath("/apps");
    return { ok: true };
  } catch (e) {
    return { error: e instanceof Error ? e.message : "删除失败" };
  }
}
