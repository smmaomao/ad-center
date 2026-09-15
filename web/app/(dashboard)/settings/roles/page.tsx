import { requireSuperAdmin } from "@/lib/auth";
import { listRoles, listMenus, getRoleMenus } from "@/lib/go-api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { ConfirmSubmit } from "@/components/confirm-submit";
import {
  createRoleAction,
  updateRoleAction,
  setRoleMenusAction,
  deleteRoleAction,
} from "./actions";

export const dynamic = "force-dynamic";

export default async function RolesPage({
  searchParams,
}: {
  searchParams?: Promise<{ error?: string }>;
}) {
  const sp = (await searchParams) ?? {};
  const session = await requireSuperAdmin();

  let roles: Awaited<ReturnType<typeof listRoles>> = [];
  let menus: Awaited<ReturnType<typeof listMenus>> = [];
  let error: string | null = null;
  try {
    [roles, menus] = await Promise.all([
      listRoles(session.email),
      listMenus(session.email),
    ]);
  } catch (e) {
    error = e instanceof Error ? e.message : "加载失败";
  }

  // 各角色已授权菜单（勾选回显）
  const granted: Record<string, string[]> = {};
  await Promise.all(
    roles.map(async (r) => {
      try {
        const res = await getRoleMenus(session.email, r.code);
        granted[r.code] = res.menu_codes ?? [];
      } catch {
        granted[r.code] = [];
      }
    }),
  );

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold">角色管理</h1>
        <p className="text-sm text-muted-foreground">
          角色决定能看到哪些菜单；保存后对应用户的侧边栏立即变化，无需改代码发版
        </p>
      </div>

      {(sp.error || error) && (
        <Card className="border-destructive">
          <CardContent className="py-3 text-sm text-destructive">
            {sp.error ?? error}
          </CardContent>
        </Card>
      )}

      <Card>
        <CardHeader>
          <CardTitle>新增角色</CardTitle>
          <CardDescription>
            标识（code）创建后不可修改——授权关系以它为锚点，改名会导致授权断裂
          </CardDescription>
        </CardHeader>
        <CardContent>
          <form
            action={createRoleAction}
            className="flex flex-wrap items-end gap-3"
          >
            <div className="space-y-1.5">
              <Label htmlFor="code">标识 *</Label>
              <Input id="code" name="code" placeholder="ops_lead" />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="name">名称 *</Label>
              <Input id="name" name="name" placeholder="运营主管" />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="description">描述</Label>
              <Input id="description" name="description" />
            </div>
            <Button type="submit">创建</Button>
          </form>
        </CardContent>
      </Card>

      {roles.map((role) => (
        <Card key={role.code}>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              {role.name}
              <span className="font-mono text-xs font-normal text-muted-foreground">
                {role.code}
              </span>
              {role.is_system && (
                <span className="rounded-full bg-zinc-100 px-2 py-0.5 text-xs font-normal text-zinc-600">
                  内置
                </span>
              )}
            </CardTitle>
            <CardDescription>
              {role.description || "—"} · 已授权 {role.menu_count} 项
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <form
              action={updateRoleAction}
              className="flex flex-wrap items-end gap-2"
            >
              <input type="hidden" name="code" value={role.code} />
              <div className="space-y-1.5">
                <Label>名称</Label>
                <Input name="name" defaultValue={role.name} className="h-8" />
              </div>
              <div className="space-y-1.5">
                <Label>描述</Label>
                <Input
                  name="description"
                  defaultValue={role.description}
                  className="h-8"
                />
              </div>
              <Button type="submit">保存</Button>
            </form>

            <form
              action={setRoleMenusAction}
              className="space-y-2 rounded-lg border p-3"
            >
              <input type="hidden" name="code" value={role.code} />
              <Label>可见菜单</Label>
              <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
                {menus.map((m) => (
                  <label
                    key={m.code}
                    className="flex items-center gap-2 text-sm"
                  >
                    <input
                      type="checkbox"
                      name="menu_codes"
                      value={m.code}
                      defaultChecked={granted[role.code]?.includes(m.code)}
                      className="size-4"
                    />
                    {m.label}
                    {!m.enabled && (
                      <span className="text-xs text-muted-foreground">
                        （已停用）
                      </span>
                    )}
                  </label>
                ))}
              </div>
              <Button type="submit">保存授权</Button>
            </form>

            {role.is_system ? (
              <p className="text-xs text-muted-foreground">
                内置角色不可删除（避免把超级管理员删掉导致后台不可用）
              </p>
            ) : (
              <form action={deleteRoleAction}>
                <input type="hidden" name="code" value={role.code} />
                <ConfirmSubmit
                  message={`确定删除角色「${role.name}」？该角色的授权关系会一并清理。`}
                >
                  删除角色
                </ConfirmSubmit>
              </form>
            )}
          </CardContent>
        </Card>
      ))}
    </div>
  );
}
