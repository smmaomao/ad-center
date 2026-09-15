"use server";

// 产品（Product）管理 Server Actions。
// 权限：页面级 requireMenu 已拦截；Go 侧对每个写接口仍复核角色。
import { revalidatePath } from "next/cache";
import { getSession } from "@/lib/auth";
import {
  createProduct,
  updateProduct,
  deleteProduct,
  GoApiError,
} from "@/lib/go-api";

export interface ProductFormState {
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

function errMsg(e: unknown, fallback: string): string {
  if (e instanceof GoApiError) return e.message;
  return e instanceof Error ? e.message : fallback;
}

/** 新建 / 编辑产品（隐藏 id 存在时为更新） */
export async function saveProductAction(
  _prev: ProductFormState,
  fd: FormData,
): Promise<ProductFormState> {
  const session = await getSession();
  if (!session) return { error: "会话已过期，请重新登录" };

  const name = str(fd, "name");
  const advertiserId = str(fd, "advertiser_id");
  if (!name) return { error: "产品名称必填" };
  if (!advertiserId) return { error: "必须选择所属广告主" };

  const fields: Record<string, unknown> = {
    advertiser_id: advertiserId,
    name,
    status: str(fd, "status") || "active",
    daily_budget: num(fd, "daily_budget"),
    notes: str(fd, "notes"),
  };

  const id = str(fd, "id");
  try {
    if (id) {
      await updateProduct(session.email, id, fields);
    } else {
      await createProduct(session.email, fields);
    }
  } catch (e) {
    return { error: errMsg(e, "保存失败") };
  }

  revalidatePath("/products");
  return { ok: true };
}

export async function deleteProductAction(fd: FormData): Promise<void> {
  const session = await getSession();
  if (!session) return;
  const id = str(fd, "id");
  if (!id) return;
  try {
    await deleteProduct(session.email, id);
  } catch {
    // 列表页会重新加载，失败无副作用
  }
  revalidatePath("/products");
}
