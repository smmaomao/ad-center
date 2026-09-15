import Link from "next/link";
import { canWriteRole, requireMenu } from "@/lib/auth";
import { listSlots, type AdminSlot } from "@/lib/go-api";
import { SLOT_TYPE_LABEL } from "./slot-form";
import { SlotRowActions } from "./row-actions";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

export const dynamic = "force-dynamic"; // 管理列表实时数据

function typeLabel(t: string): string {
  return SLOT_TYPE_LABEL[t] ?? t;
}

function statusBadge(status: string): string {
  return status === "active"
    ? "bg-emerald-50 text-emerald-700"
    : "bg-zinc-100 text-zinc-600";
}

export default async function SlotsPage() {
  const session = await requireMenu("/slots");
  const canWrite = canWriteRole(session.role);

  let slots: AdminSlot[] = [];
  let error: string | null = null;
  try {
    slots = await listSlots(session.email);
  } catch (e) {
    error = e instanceof Error ? e.message : "加载失败";
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold">广告位管理</h1>
          <p className="text-sm text-muted-foreground">
            共 {slots.length} 个广告位，填充优先级在详情页配置
          </p>
        </div>
        {canWrite && (
          <Link href="/slots/new">
            <Button>新增广告位</Button>
          </Link>
        )}
      </div>

      {error && (
        <Card className="border-destructive">
          <CardHeader>
            <CardTitle className="text-destructive">加载失败</CardTitle>
            <CardDescription>{error}</CardDescription>
          </CardHeader>
        </Card>
      )}

      {slots.length > 0 && (
        <Card>
          <CardContent className="p-0">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b text-left text-muted-foreground">
                  <th className="px-4 py-3 font-medium">广告位</th>
                  <th className="px-4 py-3 font-medium">App</th>
                  <th className="px-4 py-3 font-medium">类型</th>
                  <th className="px-4 py-3 font-medium">状态</th>
                  <th className="px-4 py-3 font-medium text-right">频控（日上限 / 间隔）</th>
                  <th className="px-4 py-3 font-medium text-right">填充来源</th>
                  <th className="px-4 py-3 font-medium">AI Agent</th>
                  <th className="px-4 py-3 text-right font-medium">操作</th>
                </tr>
              </thead>
              <tbody>
                {slots.map((s) => (
                  <tr key={s.id} className="border-b last:border-0 hover:bg-muted/30">
                    <td className="px-4 py-3">
                      <Link
                        href={`/slots/${s.id}`}
                        className="font-medium hover:underline"
                      >
                        {s.name}
                      </Link>
                      <div className="font-mono text-xs text-muted-foreground">
                        {s.key}
                      </div>
                    </td>
                    <td className="px-4 py-3">{s.app_name}</td>
                    <td className="px-4 py-3">{typeLabel(s.type)}</td>
                    <td className="px-4 py-3">
                      <span
                        className={`inline-flex rounded-full px-2 py-0.5 text-xs ${statusBadge(s.status)}`}
                      >
                        {s.status === "active" ? "启用" : "停用"}
                      </span>
                    </td>
                    <td className="px-4 py-3 text-right tabular-nums text-muted-foreground">
                      {s.freq_daily_limit > 0 ? `${s.freq_daily_limit} 次` : "不限"}
                      {" / "}
                      {s.freq_interval_minutes > 0 ? `${s.freq_interval_minutes} 分` : "不限"}
                    </td>
                    <td className="px-4 py-3 text-right tabular-nums">
                      {s.fill_count}
                    </td>
                    <td className="px-4 py-3">
                      {s.ai_agent_enabled ? (
                        <span className="text-xs font-medium text-violet-700">
                          已接管
                        </span>
                      ) : (
                        <span className="text-xs text-muted-foreground">—</span>
                      )}
                    </td>
                    <td className="px-4 py-3">
                      <SlotRowActions
                        id={s.id}
                        name={s.name}
                        canDelete={canWrite}
                      />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </CardContent>
        </Card>
      )}

      {!error && slots.length === 0 && (
        <Card>
          <CardContent className="py-10 text-center text-sm text-muted-foreground">
            还没有广告位，点击右上角「新增广告位」创建
          </CardContent>
        </Card>
      )}
    </div>
  );
}
