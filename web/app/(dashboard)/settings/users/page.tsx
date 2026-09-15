import { requireSuperAdmin } from "@/lib/auth";
import { listUsers, listRoles } from "@/lib/go-api";
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
  createUserAction,
  updateUserAction,
  deleteUserAction,
} from "./actions";

export const dynamic = "force-dynamic";

const selectCls =
  "h-8 rounded-lg border border-input bg-transparent px-2 text-sm outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50";

export default async function UsersPage({
  searchParams,
}: {
  searchParams?: Promise<{ error?: string }>;
}) {
  const sp = (await searchParams) ?? {};
  const session = await requireSuperAdmin();

  let users: Awaited<ReturnType<typeof listUsers>> = [];
  let roles: Awaited<ReturnType<typeof listRoles>> = [];
  let error: string | null = null;
  try {
    [users, roles] = await Promise.all([
      listUsers(session.email),
      listRoles(session.email),
    ]);
  } catch (e) {
    error = e instanceof Error ? e.message : "加载失败";
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold">用户管理</h1>
        <p className="text-sm text-muted-foreground">
          分配角色、启用或禁用后台账号；删除只解除后台授权，不影响 Supabase Auth 账号
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
          <CardTitle>新增用户</CardTitle>
          <CardDescription>
            邮箱需已注册 Supabase Auth（让该用户先登录一次），否则无法绑定后台账号
          </CardDescription>
        </CardHeader>
        <CardContent>
          <form
            action={createUserAction}
            className="flex flex-wrap items-end gap-3"
          >
            <div className="space-y-1.5">
              <Label htmlFor="email">邮箱 *</Label>
              <Input
                id="email"
                name="email"
                type="email"
                required
                placeholder="ops@example.com"
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="role">角色 *</Label>
              <select
                id="role"
                name="role"
                className={selectCls}
                defaultValue="operator"
              >
                {roles.map((r) => (
                  <option key={r.code} value={r.code}>
                    {r.name}
                  </option>
                ))}
              </select>
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="status">状态</Label>
              <select
                id="status"
                name="status"
                className={selectCls}
                defaultValue="active"
              >
                <option value="active">启用</option>
                <option value="disabled">禁用</option>
              </select>
            </div>
            <Button type="submit">添加</Button>
          </form>
        </CardContent>
      </Card>

      <Card>
        <CardContent className="p-0">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b text-left text-muted-foreground">
                <th className="px-4 py-3 font-medium">邮箱</th>
                <th className="px-4 py-3 font-medium">角色 / 状态</th>
                <th className="px-4 py-3 font-medium text-right">操作</th>
              </tr>
            </thead>
            <tbody>
              {users.map((u) => (
                <tr key={u.auth_user_id} className="border-b last:border-0">
                  <td className="px-4 py-3">
                    <div className="font-medium">{u.email}</div>
                    <div className="font-mono text-xs text-muted-foreground">
                      {u.auth_user_id}
                    </div>
                  </td>
                  <td className="px-4 py-3">
                    <form
                      action={updateUserAction}
                      className="flex flex-wrap items-center gap-2"
                    >
                      <input type="hidden" name="id" value={u.auth_user_id} />
                      <select
                        name="role"
                        className={selectCls}
                        defaultValue={u.role}
                      >
                        {roles.map((r) => (
                          <option key={r.code} value={r.code}>
                            {r.name}
                          </option>
                        ))}
                      </select>
                      <select
                        name="status"
                        className={selectCls}
                        defaultValue={u.status}
                      >
                        <option value="active">启用</option>
                        <option value="disabled">禁用</option>
                      </select>
                      <Button type="submit">保存</Button>
                    </form>
                  </td>
                  <td className="px-4 py-3 text-right">
                    <form action={deleteUserAction}>
                      <input type="hidden" name="id" value={u.auth_user_id} />
                      <ConfirmSubmit
                        message={`确定移除「${u.email}」的后台授权？`}
                      >
                        删除
                      </ConfirmSubmit>
                    </form>
                  </td>
                </tr>
              ))}
              {users.length === 0 && !error && (
                <tr>
                  <td
                    colSpan={3}
                    className="px-4 py-10 text-center text-sm text-muted-foreground"
                  >
                    还没有后台用户
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </CardContent>
      </Card>
    </div>
  );
}
