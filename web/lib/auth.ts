import { redirect } from "next/navigation";
import { createClient } from "@/lib/supabase/server";

/** 后台角色（对应 PRD 第三章 + ads_center.admin_users.role） */
export type Role = "super_admin" | "operator" | "analyst" | "strategy";

export const ROLE_LABELS: Record<Role, string> = {
  super_admin: "超级管理员",
  operator: "广告运营",
  analyst: "数据分析",
  strategy: "产品/策略",
};

/** 侧边栏导航项 */
export interface NavItem {
  href: string;
  label: string;
}

export const NAV_ITEMS: NavItem[] = [
  { href: "/dashboard", label: "实时监控" },
  { href: "/advertisers", label: "广告主管理" },
  { href: "/slots", label: "广告位管理" },
  { href: "/ai-agent", label: "AI Agent 控制台" },
];

/** 角色 → 可访问路由前缀（PRD 第三章权限矩阵） */
export const ROLE_ROUTES: Record<Role, string[]> = {
  super_admin: ["/dashboard", "/advertisers", "/slots", "/ai-agent"],
  operator: ["/dashboard", "/advertisers", "/slots", "/ai-agent"],
  analyst: ["/dashboard", "/ai-agent"],
  strategy: ["/dashboard", "/ai-agent"],
};

/** 角色落地页（无权限时的回退目标） */
export function homeRoute(role: Role): string {
  return ROLE_ROUTES[role][0];
}

export function roleCanAccess(role: Role, pathname: string): boolean {
  return ROLE_ROUTES[role].some(
    (r) => pathname === r || pathname.startsWith(r + "/"),
  );
}

export function navItemsForRole(role: Role): NavItem[] {
  return NAV_ITEMS.filter((item) => roleCanAccess(role, item.href));
}

export interface Session {
  email: string;
  role: Role;
}

/** 获取会话：已登录且在 admin_users 中有角色才返回 Session */
export async function getSession(): Promise<Session | null> {
  const supabase = await createClient();
  const {
    data: { user },
  } = await supabase.auth.getUser();
  if (!user?.email) return null;

  const { data: role } = await supabase.rpc("current_admin_role");
  if (!role) return null;

  return { email: user.email, role: role as Role };
}

/**
 * 页面级 RBAC 守卫：
 * - 未登录 → /login
 * - 已登录但不在 admin_users → /login?error=no_access
 * - 角色无权限 → 重定向到该角色的落地页
 */
export async function requireRole(allowed: Role[]): Promise<Session> {
  const session = await getSession();
  if (!session) redirect("/login?error=no_access");
  if (!allowed.includes(session.role)) redirect(homeRoute(session.role));
  return session;
}
