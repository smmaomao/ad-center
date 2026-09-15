"use server";

// 菜单管理 Server Actions。
// 权限：页面 requireSuperAdmin 已拦截；Go 侧对每个写接口仍复核 super_admin。
import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";
import { getSession } from "@/lib/auth";
import { createMenu, updateMenu, deleteMenu, GoApiError } from "@/lib/go-api";

function str(fd: FormData, key: string): string {
  const v = fd.get(key);
  return typeof v === "string" ? v.trim() : "";
}

function num(fd: FormData, key: string): number {
  const v = Number.parseInt(str(fd, key), 10);
  return Number.isFinite(v) ? v : 0;
}

function fail(msg: string): never {
  redirect("/settings/menus?error=" + encodeURIComponent(msg));
}

async function actor(): Promise<string> {
  const session = await getSession();
  if (!session) redirect("/login?error=no_access");
  return session.email;
}

export async function createMenuAction(fd: FormData) {
  const code = str(fd, "code");
  const label = str(fd, "label");
  if (!code || !label) fail("菜单标识与名称必填");

  let err: string | null = null;
  try {
    await createMenu(await actor(), {
      code,
      label,
      href: str(fd, "href"),
      parent_code: str(fd, "parent_code"),
      sort_order: num(fd, "sort_order"),
      enabled: fd.get("enabled") !== null,
    });
  } catch (e) {
    err = e instanceof GoApiError ? e.message : "创建失败";
  }
  if (err) fail(err);
  revalidatePath("/settings/menus");
}

export async function updateMenuAction(fd: FormData) {
  const code = str(fd, "code");
  if (!code) fail("缺少菜单标识");

  let err: string | null = null;
  try {
    await updateMenu(await actor(), code, {
      label: str(fd, "label"),
      href: str(fd, "href"),
      parent_code: str(fd, "parent_code"),
      sort_order: num(fd, "sort_order"),
      enabled: fd.get("enabled") !== null,
    });
  } catch (e) {
    err = e instanceof GoApiError ? e.message : "保存失败";
  }
  if (err) fail(err);
  revalidatePath("/settings/menus");
}

export async function deleteMenuAction(fd: FormData) {
  const code = str(fd, "code");
  if (!code) fail("缺少菜单标识");

  let err: string | null = null;
  try {
    await deleteMenu(await actor(), code);
  } catch (e) {
    err = e instanceof GoApiError ? e.message : "删除失败";
  }
  if (err) fail(err);
  revalidatePath("/settings/menus");
}
