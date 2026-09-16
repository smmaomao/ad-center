"use server";

// 用户管理 Server Actions。
// 权限：页面 requireSuperAdmin 已拦截；Go 侧对每个写接口仍复核 super_admin。
import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";
import { getSession } from "@/lib/auth";
import {
  createUser,
  updateUser,
  deleteUser,
  GoApiError,
} from "@/lib/go-api";

function str(fd: FormData, key: string): string {
  const v = fd.get(key);
  return typeof v === "string" ? v.trim() : "";
}

/** 失败回跳列表并带上错误文案（页面从 searchParams 读取展示） */
function fail(msg: string): never {
  redirect("/settings/users?error=" + encodeURIComponent(msg));
}

async function actor(): Promise<string> {
  const session = await getSession();
  if (!session) redirect("/login?error=no_access");
  return session.email;
}

export async function createUserAction(fd: FormData) {
  const email = str(fd, "email");
  const role = str(fd, "role");
  if (!email || !role) fail("邮箱与角色必填");

  let err: string | null = null;
  try {
    await createUser(await actor(), {
      email,
      role,
      status: str(fd, "status") || "active",
      password: str(fd, "password") || undefined,
    });
  } catch (e) {
    err = e instanceof GoApiError ? e.message : "添加失败";
  }
  if (err) fail(err);
  revalidatePath("/settings/users");
}

export async function updateUserAction(fd: FormData) {
  const id = str(fd, "id");
  if (!id) fail("缺少用户 ID");

  let err: string | null = null;
  try {
    await updateUser(await actor(), id, {
      role: str(fd, "role"),
      status: str(fd, "status"),
      password: str(fd, "password") || undefined,
    });
  } catch (e) {
    err = e instanceof GoApiError ? e.message : "保存失败";
  }
  if (err) fail(err);
  revalidatePath("/settings/users");
}

export async function deleteUserAction(fd: FormData) {
  const id = str(fd, "id");
  if (!id) fail("缺少用户 ID");

  let err: string | null = null;
  try {
    await deleteUser(await actor(), id);
  } catch (e) {
    err = e instanceof GoApiError ? e.message : "删除失败";
  }
  if (err) fail(err);
  revalidatePath("/settings/users");
}
