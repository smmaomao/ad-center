import { requireRole } from "@/lib/auth";
import { listAdvertisers, type AdminAdvertiser } from "@/lib/go-api";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

export const dynamic = "force-dynamic"; // 管理列表实时数据

/** 状态预警规则（PRD 4.1）：预算将尽（≥95%）/ KPI 未达标（<85%）高亮 */
function alerts(a: AdminAdvertiser): string[] {
  const out: string[] = [];
  if (a.daily_budget > 0 && a.spent_today / a.daily_budget >= 0.95) {
    out.push("预算将尽");
  }
  if (a.achievement < 0.85) {
    out.push("KPI 未达标");
  }
  return out;
}

function statusBadge(status: AdminAdvertiser["status"]): string {
  switch (status) {
    case "active":
      return "bg-emerald-50 text-emerald-700";
    case "paused":
      return "bg-zinc-100 text-zinc-600";
    case "budget_exhausted":
      return "bg-red-50 text-red-700";
  }
}

const STATUS_LABEL: Record<AdminAdvertiser["status"], string> = {
  active: "投放中",
  paused: "已暂停",
  budget_exhausted: "预算耗尽",
};

export default async function AdvertisersPage() {
  const session = await requireRole(["super_admin", "operator", "strategy"]);

  let advertisers: AdminAdvertiser[] = [];
  let error: string | null = null;
  try {
    advertisers = await listAdvertisers(session.email);
  } catch (e) {
    error = e instanceof Error ? e.message : "加载失败";
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold">广告主管理</h1>
        <p className="text-sm text-muted-foreground">
          共 {advertisers.length} 个广告主，按 Tier 分层
        </p>
      </div>

      {error && (
        <Card className="border-destructive">
          <CardHeader>
            <CardTitle className="text-destructive">加载失败</CardTitle>
            <CardDescription>{error}</CardDescription>
          </CardHeader>
        </Card>
      )}

      {advertisers.length > 0 && (
        <Card>
          <CardContent className="p-0">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b text-left text-muted-foreground">
                  <th className="px-4 py-3 font-medium">广告主</th>
                  <th className="px-4 py-3 font-medium">Tier</th>
                  <th className="px-4 py-3 font-medium">状态</th>
                  <th className="px-4 py-3 font-medium text-right">
                    KPI 达成率
                  </th>
                  <th className="px-4 py-3 font-medium text-right">
                    今日预算
                  </th>
                  <th className="px-4 py-3 font-medium">预警</th>
                </tr>
              </thead>
              <tbody>
                {advertisers.map((a) => {
                  const warn = alerts(a);
                  return (
                    <tr
                      key={a.id}
                      className={`border-b last:border-0 ${warn.length > 0 ? "bg-amber-50/50" : ""}`}
                    >
                      <td className="px-4 py-3">
                        <div className="font-medium">{a.name}</div>
                        {a.contact && (
                          <div className="text-xs text-muted-foreground">
                            {a.contact}
                          </div>
                        )}
                      </td>
                      <td className="px-4 py-3">T{a.tier}</td>
                      <td className="px-4 py-3">
                        <span
                          className={`inline-flex rounded-full px-2 py-0.5 text-xs ${statusBadge(a.status)}`}
                        >
                          {STATUS_LABEL[a.status]}
                        </span>
                      </td>
                      <td className="px-4 py-3 text-right">
                        <span
                          className={
                            a.achievement < 0.85
                              ? "font-medium text-red-600"
                              : "text-emerald-700"
                          }
                        >
                          {(a.achievement * 100).toFixed(0)}%
                        </span>
                      </td>
                      <td className="px-4 py-3 text-right tabular-nums">
                        ${a.spent_today.toFixed(2)} / $
                        {a.daily_budget.toFixed(2)}
                      </td>
                      <td className="px-4 py-3">
                        {warn.length > 0 ? (
                          <span className="text-xs font-medium text-amber-700">
                            {warn.join(" · ")}
                          </span>
                        ) : (
                          <span className="text-xs text-muted-foreground">
                            —
                          </span>
                        )}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
