"use server";

// 广告素材管理 Server Actions。
// 权限：页面级 requireMenu 已拦截；Go 侧对每个写接口仍复核角色。
import { redirect } from "next/navigation";
import { revalidatePath } from "next/cache";
import { getSession } from "@/lib/auth";
import {
  createCreative,
  updateCreative,
  deleteCreative,
  getCreativePlayUrl,
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

/** 新建 / 编辑素材（隐藏 id 存在时为更新） */
export async function saveCreativeAction(
  _prev: FormState,
  fd: FormData,
): Promise<FormState> {
  const session = await getSession();
  if (!session) return { error: "会话已过期，请重新登录" };

  const name = str(fd, "name");
  const advertiserId = str(fd, "advertiser_id");
  if (!name) return { error: "素材名称必填" };
  // 广告主可空：空 = 公共素材库

  // 展现样式多选
  const styles = fd.getAll("styles").map(String).filter(Boolean);
  if (styles.length === 0) return { error: "至少勾选一个展现样式" };

  const storagePath = str(fd, "storage_path");
  if (!storagePath) return { error: "素材地址必填（上传后的对象 key 或落地页 URL）" };

  const fields: Record<string, unknown> = {
    advertiser_id: advertiserId,
    name,
    media_type: str(fd, "media_type") || "video",
    storage_path: storagePath,
    status: str(fd, "status") || "active",
    orientation: str(fd, "orientation") || "any",
    styles,
    // 投放目标：空数组 = 全部 App
    target_apps: fd.getAll("target_apps").map(String).filter(Boolean),
  };

  // 媒体元数据（前端自动解析后随表单带来；仅在上传时带出，避免编辑时回退已有值）
  const widthRaw = str(fd, "width");
  const heightRaw = str(fd, "height");
  const durationRaw = str(fd, "duration_ms");
  const sizeRaw = str(fd, "file_size_bytes");
  if (widthRaw) fields.width = Math.round(Number(widthRaw));
  if (heightRaw) fields.height = Math.round(Number(heightRaw));
  if (durationRaw) fields.duration_ms = Math.round(Number(durationRaw));
  if (sizeRaw) fields.file_size_bytes = Number(sizeRaw);

  const id = str(fd, "id");
  try {
    if (id) {
      await updateCreative(session.email, id, fields);
    } else {
      await createCreative(session.email, fields);
    }
  } catch (e) {
    return { error: e instanceof GoApiError ? e.message : "保存失败" };
  }

  revalidatePath("/creatives");
  if (!id) {
    // 弹窗内提交（带 no_redirect）不跳转，交由前端关闭弹窗并刷新
    if (fd.get("no_redirect")) return { ok: true };
    redirect("/creatives");
  }
  revalidatePath(`/creatives/${id}`);
  return { ok: true };
}

export async function deleteCreativeAction(fd: FormData): Promise<void> {
  const session = await getSession();
  if (!session) return;
  const id = str(fd, "id");
  if (!id) return;
  try {
    await deleteCreative(session.email, id);
  } catch {
    // 忽略：列表页会重新加载，失败也无副作用
  }
  revalidatePath("/creatives");
  redirect("/creatives");
}

/** 后台素材预览：服务端调 Go 签出可播放地址（避免浏览器直连 Go） */
export async function getCreativePlayUrlAction(
  id: string,
): Promise<{ url: string; media_type: string }> {
  const session = await getSession();
  if (!session) throw new Error("会话已过期，请重新登录");
  return getCreativePlayUrl(session.email, id);
}
