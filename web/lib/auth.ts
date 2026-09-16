import { cache } from "react";
import { redirect } from "next/navigation";
import { cookies } from "next/headers";
import { GO_API_URL, INTERNAL_API_KEY } from "@/lib/go-api";

/**
 * 后台角色（对应 ads_center.admin_users.role）。
 * 000016 起角色可在后台「角色管理」里自定义，因此不再限定为 4 个字面量。
 */
export type Role = string;

/** 内置角色的中文名；自定义角色未配置时回退显示 code */
export const ROLE_LABELS: Record<string, string> = {
  super_admin: "超级管理员",
  operator: "广告运营",
  analyst: "数据分析",
  strategy: "产品/策略",
};

export function roleLabel(role: Role): string {
  return ROLE_LABELS[role] ?? role;
}

export interface NavItem {
  href: string;
  label: string;
  code?: string;
}

/**
 * 兜底导航：只有在「库里读不到菜单」时才用（迁移未执行 / DB 故障），
 * 保证后台不会因为这些原因变成空白页。
 */
const FALLBACK_NAV: NavItem[] = [
  { href: "/dashboard", label: "实时监控" },
  { href: "/advertisers", label: "广告主管理" },
  { href: "/campaigns", label: "广告任务管理" },
  { href: "/creatives", label: "素材管理" },
  { href: "/products", label: "产品管理" },
  { href: "/slots", label: "广告位管理" },
  { href: "/ai-agent", label: "AI Agent 控制台" },
  { href: "/settings/pricing", label: "平台计费标准线" },
  { href: "/settings/cache", label: "决策结果缓存" },
  { href: "/settings/fatigue", label: "全局频控配置" },
];

/**
 * 角色 → 可访问路由前缀。
 * 现在仅作为兜底与 homeRoute 使用；侧边栏与可见性以库里的菜单授权为准（getNavItems）。
 */
export const ROLE_ROUTES: Record<string, string[]> = {
  super_admin: ["/dashboard", "/advertisers", "/campaigns", "/creatives", "/slots", "/ai-agent", "/settings"],
  operator: ["/dashboard", "/advertisers", "/campaigns", "/creatives", "/slots", "/ai-agent", "/settings"],
  analyst: ["/dashboard", "/ai-agent"],
  strategy: ["/dashboard", "/ai-agent"],
};

interface MenuRow {
  menu_code: string;
  menu_label: string;
  menu_href: string | null;
  menu_parent_code: string | null;
  menu_sort_order: number;
}

/** 侧边栏树形节点：有 children 即分组；叶子必须有 href */
export interface NavNode {
  code: string;
  label: string;
  href: string | null;
  parentCode: string | null;
  sortOrder: number;
  children: NavNode[];
}

/** 兜底导航树：迁移未执行或 DB 故障时不至于让后台变成空白 */
const FALLBACK_TREE: NavNode[] = [
  { code: "dashboard", label: "实时监控", href: "/dashboard", parentCode: null, sortOrder: 10, children: [] },
  { code: "apps", label: "应用管理", href: "/apps", parentCode: null, sortOrder: 15, children: [] },
  {
    code: "ad_mgmt",
    label: "广告管理",
    href: null,
    parentCode: null,
    sortOrder: 20,
    children: [
      {
        code: "advertisers",
        label: "广告主管理",
        href: "/advertisers",
        parentCode: "ad_mgmt",
        sortOrder: 10,
        children: [],
      },
      {
        code: "products",
        label: "产品管理",
        href: "/products",
        parentCode: "ad_mgmt",
        sortOrder: 15,
        children: [],
      },
      {
        code: "campaigns",
        label: "广告任务管理",
        href: "/campaigns",
        parentCode: "ad_mgmt",
        sortOrder: 20,
        children: [],
      },
      {
        code: "creatives",
        label: "素材管理",
        href: "/creatives",
        parentCode: "ad_mgmt",
        sortOrder: 30,
        children: [],
      },
    ],
  },
  { code: "slots", label: "广告位管理", href: "/slots", parentCode: null, sortOrder: 30, children: [] },
  { code: "ai-agent", label: "AI Agent 控制台", href: "/ai-agent", parentCode: null, sortOrder: 40, children: [] },
  {
    code: "other_config",
    label: "其他配置",
    href: null,
    parentCode: null,
    sortOrder: 45,
    children: [
      {
        code: "pricing_benchmark",
        label: "平台计费标准线",
        href: "/settings/pricing",
        parentCode: "other_config",
        sortOrder: 10,
        children: [],
      },
      {
        code: "decision_cache",
        label: "决策结果缓存",
        href: "/settings/cache",
        parentCode: "other_config",
        sortOrder: 20,
        children: [],
      },
      {
        code: "fatigue",
        label: "全局频控配置",
        href: "/settings/fatigue",
        parentCode: "other_config",
        sortOrder: 30,
        children: [],
      },
    ],
  },
  {
    code: "settings",
    label: "系统设置",
    href: null,
    parentCode: null,
    sortOrder: 50,
    children: [
      {
        code: "system.users",
        label: "用户管理",
        href: "/settings/users",
        parentCode: "settings",
        sortOrder: 10,
        children: [],
      },
      {
        code: "system.roles",
        label: "角色管理",
        href: "/settings/roles",
        parentCode: "settings",
        sortOrder: 20,
        children: [],
      },
      {
        code: "system.menus",
        label: "菜单管理",
        href: "/settings/menus",
        parentCode: "settings",
        sortOrder: 30,
        children: [],
      },
    ],
  },
];

/** 把扁平 RPC 行拼成树：按 parent_code 关联，孤儿节点（parent 不在授权链里）作根 */
function buildTree(rows: MenuRow[]): NavNode[] {
  const map = new Map<string, NavNode>();
  rows.forEach((r) => {
    map.set(r.menu_code, {
      code: r.menu_code,
      label: r.menu_label,
      href: r.menu_href,
      parentCode: r.menu_parent_code,
      sortOrder: r.menu_sort_order,
      children: [],
    });
  });
  const roots: NavNode[] = [];
  map.forEach((n) => {
    if (n.parentCode && map.has(n.parentCode)) {
      map.get(n.parentCode)!.children.push(n);
    } else {
      roots.push(n);
    }
  });
  const cmp = (a: NavNode, b: NavNode) =>
    a.sortOrder - b.sortOrder || a.code.localeCompare(b.code);
  roots.sort(cmp);
  roots.forEach((r) => r.children.sort(cmp));
  return roots;
}

/**
 * 当前登录用户可见菜单树 —— **DB 驱动**（migration 000019 的 current_admin_menus
 * RPC，已递归返父链）。后台「菜单管理 / 角色管理」改动后无需发版即可生效。
 *
 * 回退策略：只有 RPC 报错才用 FALLBACK_TREE；用户被禁用或角色未授权任何菜单时
 * 返回空数组 —— 这种情况**不能**回退，否则等于给被禁用账号放行。
 *
 * 用 React cache 包一层：同一次请求内 layout 与各页面只查一次库。
 */
interface MeResponse {
  email: string;
  role: string;
  menu: MenuRow[];
}

/**
 * 凭 cookie 里的会话令牌向 Go 取当前用户身份 + 菜单。
 * Go 验签令牌（HMAC）作为权威，前端不持有密钥、不做本地验签。
 * 用 React cache 包一层：同一次请求内 layout / 各页面只打一次 Go。
 */
const getMe = cache(async (): Promise<MeResponse | null> => {
  const token = (await cookies()).get("ad_session")?.value;
  if (!token) return null;
  try {
    const res = await fetch(GO_API_URL + "/v1/admin/me", {
      headers: {
        Authorization: "Bearer " + token,
        "X-Internal-Key": INTERNAL_API_KEY,
      },
      cache: "no-store",
    });
    if (!res.ok) return null;
    return (await res.json()) as MeResponse;
  } catch {
    return null;
  }
});

export const getNavTree = cache(async (): Promise<NavNode[]> => {
  const me = await getMe();
  if (!me || !me.menu || me.menu.length === 0) return FALLBACK_TREE;
  return buildTree(me.menu);
});

/** 扁平叶子（给 requireMenu 等只关心路由的场景用），从 getNavTree 提取 */
export const getNavItems = cache(async (): Promise<NavItem[]> => {
  const tree = await getNavTree();
  const out: NavItem[] = [];
  const walk = (n: NavNode) => {
    if (n.href) out.push({ code: n.code, label: n.label, href: n.href });
    n.children.forEach(walk);
  };
  tree.forEach(walk);
  return out.length > 0 ? out : FALLBACK_NAV;
});

export function homeRoute(role: Role): string {
  return ROLE_ROUTES[role]?.[0] ?? "/dashboard";
}

export function roleCanAccess(role: Role, pathname: string): boolean {
  return (ROLE_ROUTES[role] ?? []).some(
    (r) => pathname === r || pathname.startsWith(r + "/"),
  );
}

export interface Session {
  email: string;
  role: Role;
}

/**
 * 获取会话：已登录、账号 active、且在 admin_users 中有角色才返回 Session。
 * 被禁用（status=disabled）的用户 current_admin_role 返回空 → 视为无会话。
 */
export async function getSession(): Promise<Session | null> {
  const me = await getMe();
  return me ? { email: me.email, role: me.role as Role } : null;
}

/**
 * 页面级 RBAC 守卫：
 * - 未登录 / 已禁用 → /login?error=no_access
 * - 角色无权限 → 重定向到该角色的落地页
 */
export async function requireRole(allowed: Role[]): Promise<Session> {
  const session = await getSession();
  if (!session) redirect("/login?error=no_access");
  if (!allowed.includes(session.role)) redirect(homeRoute(session.role));
  return session;
}

/** 仅超级管理员可进入（用户 / 角色 / 菜单管理） */
export async function requireSuperAdmin(): Promise<Session> {
  return requireRole(["super_admin"]);
}

/**
 * 页面级访问守卫 —— 与侧边栏同一个数据源（库里的菜单授权）。
 *
 * 为什么不用 requireRole(角色白名单)：角色现在可以在后台自定义，而页面里的
 * 角色白名单是写死的，新建的角色永远不在任何白名单里 → 能登录却进不去任何页面，
 * 角色管理等于白做。改成按菜单判定后，授权勾到哪，入口就开到哪。
 */
export async function requireMenu(pathname: string): Promise<Session> {
  const session = await getSession();
  if (!session) redirect("/login?error=no_access");

  const items = await getNavItems();
  const ok = items.some(
    (i) => pathname === i.href || pathname.startsWith(i.href + "/"),
  );
  if (!ok) redirect(items[0]?.href ?? "/dashboard");
  return session;
}

/**
 * 是否有写权限（新增 / 修改 / 删除）。
 *
 * 菜单授权只解决「能不能进这个页面」；写操作更敏感，仍按内置角色收口——
 * 自定义角色默认只读，避免随手勾个菜单就拿到改广告主 / 广告位的权限。
 * 前端隐藏按钮只是体验，真正的拦截在 Go 侧（requireRole needWrite）。
 */
export function canWriteRole(role: Role): boolean {
  return role === "super_admin" || role === "operator";
}
