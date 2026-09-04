import { requireRole } from "@/lib/auth";
import { getAdvertiser, listCreatives, type AdminAdvertiser } from "@/lib/go-api";
import { AdvertiserForm } from "../advertiser-form";
import { CreativesManager } from "../creatives-manager";
import { DeleteEntityButton } from "@/components/danger-zone";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

export const dynamic = "force-dynamic"; // KPI/消耗实时数据

const BIDDING_LABEL: Record<string, string> = {
  cpi: "CPI · 按安装",
  cpa: "CPA · 按行为",
  revenue_share: "收入分成",
};

const SPEED_LABEL: Record<string, string> = {
  even: "均匀消耗",
  accelerated: "加速消耗",
  asap: "尽快花完",
};

function kpiCard(
  title: string,
  value: string,
  sub?: string,
  warn?: boolean,
): React.ReactElement {
  return (
    <Card>
      <CardHeader>
        <CardDescription>{title}</CardDescription>
        <CardTitle className={`tabular-nums ${warn ? "text-red-600" : ""}`}>
          {value}
        </CardTitle>
      </CardHeader>
      {sub && (
        <CardContent className="text-xs text-muted-foreground">{sub}</CardContent>
      )}
    </Card>
  );
}

export default async function AdvertiserDetailPage({
  params,
}: PageProps<"/advertisers/[id]">) {
  const session = await requireRole(["super_admin", "operator"]);
  const { id } = await params;

  let advertiser: AdminAdvertiser | null = null;
  let loadError: string | null = null;
  try {
    advertiser = await getAdvertiser(session.email, id);
  } catch (e) {
    loadError = e instanceof Error ? e.message : "加载失败";
  }
  if (loadError || !advertiser) {
    return (
      <Card className="border-destructive">
        <CardHeader>
          <CardTitle className="text-destructive">加载失败</CardTitle>
          <CardDescription>{loadError ?? "广告主不存在"}</CardDescription>
        </CardHeader>
      </Card>
    );
  }

  const creatives = await listCreatives(session.email, id).catch(() => []);
  const budgetPct =
    advertiser.daily_budget > 0
      ? Math.min(100, (advertiser.spent_today / advertiser.daily_budget) * 100)
      : 0;
  const budgetNearLimit =
    advertiser.daily_budget > 0 &&
    advertiser.spent_today / advertiser.daily_budget >= 0.95;

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between">
        <div>
          <h1 className="flex items-center gap-2 text-2xl font-semibold">
            {advertiser.name}
            <span className="rounded-full bg-muted px-2 py-0.5 text-xs font-medium text-muted-foreground">
              T{advertiser.tier}
            </span>
            <span
              className={`rounded-full px-2 py-0.5 text-xs font-medium ${
                advertiser.status === "active"
                  ? "bg-emerald-50 text-emerald-700"
                  : advertiser.status === "paused"
                    ? "bg-zinc-100 text-zinc-600"
                    : "bg-red-50 text-red-700"
              }`}
            >
              {advertiser.status === "active"
                ? "投放中"
                : advertiser.status === "paused"
                  ? "已暂停"
                  : "预算耗尽"}
            </span>
          </h1>
          <p className="text-sm text-muted-foreground">
            {advertiser.contact || "未填写联系方式"}
            {advertiser.end_at && ` · 投放至 ${advertiser.end_at.slice(0, 10)}`}
            {" · "}
            {SPEED_LABEL[advertiser.consume_speed] ?? advertiser.consume_speed}
          </p>
        </div>
        {session.role === "super_admin" && (
          <DeleteEntityButton kind="advertiser" id={id} label={advertiser.name} />
        )}
      </div>

      {/* KPI 卡片（PRD 4.1）：预算进度 / KPI 达成 / 出价 / 保量 */}
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <Card>
          <CardHeader>
            <CardDescription>今日预算消耗</CardDescription>
            <CardTitle className={`tabular-nums ${budgetNearLimit ? "text-red-600" : ""}`}>
              ${advertiser.spent_today.toFixed(2)}
              {advertiser.daily_budget > 0 && (
                <span className="text-sm font-normal text-muted-foreground">
                  {" "}
                  / ${advertiser.daily_budget.toFixed(2)}
                </span>
              )}
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="h-2 overflow-hidden rounded-full bg-muted">
              <div
                className={`h-full rounded-full ${budgetNearLimit ? "bg-red-500" : "bg-emerald-500"}`}
                style={{ width: `${budgetPct}%` }}
              />
            </div>
          </CardContent>
        </Card>
        {kpiCard(
          "KPI 达成率",
          `${(advertiser.achievement * 100).toFixed(0)}%`,
          `实际 CPI $${advertiser.actual_cpi.toFixed(2)} / 目标 $${advertiser.target_cpi.toFixed(2)}`,
          advertiser.achievement < 0.85,
        )}
        {kpiCard(
          "出价",
          `$${advertiser.bidding_price.toFixed(2)}`,
          BIDDING_LABEL[advertiser.bidding_mode] ?? advertiser.bidding_mode,
        )}
        {kpiCard(
          "保量份额",
          advertiser.guaranteed_enabled
            ? `${(advertiser.guaranteed_min_share * 100).toFixed(0)}%`
            : "未启用",
          "保量广告位最小填充份额",
        )}
      </div>

      <AdvertiserForm initial={advertiser} />

      <CreativesManager advertiserId={id} creatives={creatives} />
    </div>
  );
}
