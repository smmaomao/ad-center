import { redirect } from "next/navigation";

export const dynamic = "force-dynamic";

// 「系统设置」是纯分组（用户 / 角色 / 菜单管理都挂在它下面），父级菜单本身不跳页。
// 仅保留此路由以兼容手输 /settings 的历史入口：落到分组第一个子页。
export default function SettingsIndex() {
  redirect("/settings/users");
}
