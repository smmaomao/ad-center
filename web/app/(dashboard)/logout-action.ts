"use server";

// 退出登录。
//
// 放在独立模块而不是 layout 里内联定义：内联 server action 的 id 依赖所在组件
// 的那次渲染，layout 被缓存/复用时容易找不到 action；独立模块导出则引用稳定。
import { redirect } from "next/navigation";
import { createClient } from "@/lib/supabase/server";

export async function logoutAction() {
  const supabase = await createClient();
  await supabase.auth.signOut();
  // 清掉会话后再跳转，避免 Router Cache 让用户看起来还在登录态
  redirect("/login");
}
