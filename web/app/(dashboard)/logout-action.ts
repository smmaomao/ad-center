"use server";

// 退出登录：清除 httpOnly 会话 cookie 后跳转（cookie 由服务端清除，
// 避免 Router Cache 让用户看起来还在登录态）。
import { redirect } from "next/navigation";
import { cookies } from "next/headers";

export async function logoutAction() {
  const jar = await cookies();
  jar.delete("ad_session");
  redirect("/login");
}
