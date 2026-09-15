"use server";

// 角色管理 Server Actions（含菜单授权）。
// 权限：页面 requireSuperAdmin 已拦截；Go 侧对每个写接口仍复核 super_admin。
import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";
import { getSession } from "@/lib/auth";
import {
  createRole,
  updateRole,
  deleteRole,
  setRoleMenus,
  GoApiError,
} from "@/lib/go-api";

function str(fd: FormData, key: string): string {
  const v = fd.get(key);
  return typeof v === "string" ? v.trim() : "";
}

function fail(msg: string): never {
  redirect("/settings/roles?error=" + encodeURIComponent(msg));
}

async function actor(): Promise<string> {
  const session = await getSession();
  if (!session) redirect("/login?error=no_access");
  return session.email;
}

export async function createRoleAction(fd: FormData) {
  const code = str(fd, "code");
  const name = str(fd, "name");
  if (!code || !name) fail("角色标识与名称必填");

  let err: string | null = null;
  try {
    await createRole(await actor(), { code, name, description: str(fd, "description") });
  } catch (e) {
    err = e instanceof GoApiError ? e.message : "创建失败";
  }
  if (err) fail(err);
  revalidatePath("/settings/roles");
}

export async function updateRoleAction(fd: FormData) {
  const code = str(fd, "code");
  if (!code) fail("缺少角色标识");

  let err: string | null = null;
  try {
    await updateRole(await actor(), code, {
      name: str(fd, "name"),
      description: str(fd, "description"),
    });
  } catch (e) {
    err = e instanceof GoApiError ? e.message : "保存失败";
  }
  if (err) fail(err);
  revalidatePath("/settings/roles");
}

/** 菜单授权：整份提交（全量替换，与填充优先级同一语义） */
export async function setRoleMenusAction(fd: FormData) {
  const code = str(fd, "code");
  if (!code) fail("缺少角色标识");
  const menuCodes = fd.getAll("menu_codes").map(String);

  let err: string | null = null;
  try {
    await setRoleMenus(await actor(), code, menuCodes);
  } catch (e) {
    err = e instanceof GoApiError ? e.message : "保存授权失败";
  }
  if (err) fail(err);
  revalidatePath("/settings/roles");
}

export async function deleteRoleAction(fd: FormData) {
  const code = str(fd, "code");
  if (!code) fail("缺少角色标识");

  let err: string | null = null;
  try {
    await deleteRole(await actor(), code);
  } catch (e) {
    err = e instanceof GoApiError ? e.message : "删除失败";
  }
  if (err) fail(err);
  revalidatePath("/settings/roles");
}
