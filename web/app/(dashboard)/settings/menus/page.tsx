import { requireSuperAdmin } from "@/lib/auth";
import { listMenus } from "@/lib/go-api";
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
import { createMenuAction, updateMenuAction, deleteMenuAction } from "./actions";

export const dynamic = "force-dynamic";

const selectCls =
  "h-8 rounded-lg border border-input bg-transparent px-2 text-sm outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50";

export default async function MenusPage({
  searchParams,
}: {
  searchParams?: Promise<{ error?: string }>;
}) {
  const sp = (await searchParams) ?? {};
  const session = await requireSuperAdmin();

  let menus: Awaited<ReturnType<typeof listMenus>> = [];
  let error: string | null = null;
  try {
    menus = await listMenus(session.email);
  } catch (e) {
    error = e instanceof Error ? e.message : "加载失败";
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold">菜单管理</h1>
        <p className="text-sm text-muted-foreground">
          控制侧边栏有哪些入口、显示顺序与是否启用；路由留空表示这是一个分组
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
          <CardTitle>新增菜单</CardTitle>
          <CardDescription>
            标识（code）创建后不可修改；父级留空即为顶层菜单
          </CardDescription>
        </CardHeader>
        <CardContent>
          <form
            action={createMenuAction}
            className="flex flex-wrap items-end gap-3"
          >
            <div className="space-y-1.5">
              <Label htmlFor="code">标识 *</Label>
              <Input id="code" name="code" placeholder="reports" />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="label">名称 *</Label>
              <Input id="label" name="label" placeholder="数据报表" />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="href">路由</Label>
              <Input id="href" name="href" placeholder="/reports（分组留空）" />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="parent_code">父级</Label>
              <select
                id="parent_code"
                name="parent_code"
                className={selectCls}
                defaultValue=""
              >
                <option value="">（顶层）</option>
                {menus
                  .filter((m) => m.href === null)
                  .map((m) => (
                    <option key={m.code} value={m.code}>
                      {m.label}
                    </option>
                  ))}
              </select>
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="sort_order">排序</Label>
              <Input
                id="sort_order"
                name="sort_order"
                type="number"
                defaultValue={100}
                className="w-20"
              />
            </div>
            <label className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                name="enabled"
                defaultChecked
                className="size-4"
              />
              启用
            </label>
            <Button type="submit">创建</Button>
          </form>
        </CardContent>
      </Card>

      <Card>
        <CardContent className="p-0">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b text-left text-muted-foreground">
                <th className="px-4 py-3 font-medium">标识 / 名称</th>
                <th className="px-4 py-3 font-medium">路由 / 父级</th>
                <th className="px-4 py-3 font-medium">排序 / 启用</th>
                <th className="px-4 py-3 font-medium text-right">操作</th>
              </tr>
            </thead>
            <tbody>
              {menus.map((m) => (
                <tr key={m.code} className="border-b last:border-0">
                  <td className="px-4 py-3" colSpan={3}>
                    <form
                      action={updateMenuAction}
                      className="flex flex-wrap items-center gap-2"
                    >
                      <input type="hidden" name="code" value={m.code} />
                      <span className="w-24 font-mono text-xs text-muted-foreground">
                        {m.code}
                      </span>
                      <Input
                        name="label"
                        defaultValue={m.label}
                        className="h-8 w-28"
                      />
                      <Input
                        name="href"
                        defaultValue={m.href ?? ""}
                        placeholder="/path（分组留空）"
                        className="h-8 w-36"
                      />
                      <select
                        name="parent_code"
                        className={selectCls}
                        defaultValue={m.parent_code ?? ""}
                      >
                        <option value="">（顶层）</option>
                        {menus
                          .filter((x) => x.code !== m.code)
                          .map((x) => (
                            <option key={x.code} value={x.code}>
                              {x.label}
                            </option>
                          ))}
                      </select>
                      <Input
                        name="sort_order"
                        type="number"
                        defaultValue={m.sort_order}
                        className="h-8 w-16"
                      />
                      <label className="flex items-center gap-1 text-sm">
                        <input
                          type="checkbox"
                          name="enabled"
                          defaultChecked={m.enabled}
                          className="size-4"
                        />
                        启用
                      </label>
                      <Button type="submit">保存</Button>
                    </form>
                  </td>
                  <td className="px-4 py-3 text-right">
                    <form action={deleteMenuAction}>
                      <input type="hidden" name="code" value={m.code} />
                      <ConfirmSubmit
                        message={`确定删除菜单「${m.label}」？子菜单与各角色的授权会一并清理。`}
                      >
                        删除
                      </ConfirmSubmit>
                    </form>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </CardContent>
      </Card>
    </div>
  );
}
