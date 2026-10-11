import Link from "next/link";
import { requireMenu, canWriteRole } from "@/lib/auth";
import { listCampaigns, getCampaignCreativeStats } from "@/lib/go-api";

export const dynamic = "force-dynamic"; // 实时数据

type SP = { campaign_id?: string; from?: string; to?: string };

function todayUTC(): string {
  return new Date().toISOString().slice(0, 10);
}

function pct(ctr: number): string {
  return (ctr * 100).toFixed(2) + "%";
}

export default async function CreativeAnalyticsPage({
  searchParams,
}: {
  searchParams: Promise<SP>;
}) {
  const sp = await searchParams;
  const session = await requireMenu("/campaigns");
  const canWrite = canWriteRole(session.role);

  const campaignID = typeof sp.campaign_id === "string" ? sp.campaign_id : "";
  const from = typeof sp.from === "string" && sp.from ? sp.from : todayUTC();
  const to = typeof sp.to === "string" && sp.to ? sp.to : todayUTC();

  let campaigns: Awaited<ReturnType<typeof listCampaigns>> = [];
  let stats: Awaited<ReturnType<typeof getCampaignCreativeStats>> | null = null;
  let error: string | null = null;

  try {
    campaigns = await listCampaigns(session.email).catch(() => []);
    stats = await getCampaignCreativeStats(session.email, {
      campaign_id: campaignID,
      from,
      to,
    });
  } catch (e) {
    error = e instanceof Error ? e.message : "加载失败";
  }

  const title =
    campaignID && stats
      ? `素材维度分析 · 任务 #${campaignID}`
      : "素材维度分析 · 全局";

  const tz = stats?.timezone || "UTC";

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold">{title}</h1>
        <Link
          href="/campaigns"
          className="text-sm text-muted-foreground hover:underline"
        >
          ← 返回广告任务
        </Link>
      </div>

      <form method="get" className="flex flex-wrap items-end gap-4 rounded-lg border p-4">
        <label className="flex flex-col gap-1 text-sm">
          广告任务
          <select
            name="campaign_id"
            defaultValue={campaignID}
            className="h-9 rounded-md border bg-background px-2"
          >
            <option value="">全部素材（全局）</option>
            {campaigns.map((c) => (
              <option key={c.id} value={c.id}>
                #{c.id} {c.name}
              </option>
            ))}
          </select>
        </label>
        <label className="flex flex-col gap-1 text-sm">
          开始日期
          <input
            type="date"
            name="from"
            defaultValue={from}
            className="h-9 rounded-md border bg-background px-2"
          />
        </label>
        <label className="flex flex-col gap-1 text-sm">
          结束日期
          <input
            type="date"
            name="to"
            defaultValue={to}
            className="h-9 rounded-md border bg-background px-2"
          />
        </label>
        <button
          type="submit"
          className="h-9 rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground"
        >
          查看
        </button>
      </form>

      {error && <p className="text-sm text-destructive">{error}</p>}

      {!stats ? (
        <p className="text-sm text-muted-foreground">暂无数据</p>
      ) : (
        <div className="space-y-3">
          <p className="text-xs text-muted-foreground">
            口径：曝光 = impression 事件，点击 = click 事件（fill 下发不计入）；
            按点击率降序。统计时区 <span className="font-medium">{tz}</span>（按天切分以此为准）。区间 {stats.from} ~ {stats.to}
          </p>
          <div className="overflow-x-auto rounded-lg border">
            <table className="w-full text-sm">
              <thead className="bg-muted">
                <tr className="text-left">
                  <th className="px-3 py-2 font-medium">素材ID</th>
                  <th className="px-3 py-2 font-medium">素材名称</th>
                  <th className="px-3 py-2 text-right font-medium">曝光量</th>
                  <th className="px-3 py-2 text-right font-medium">点击量</th>
                  <th className="px-3 py-2 text-right font-medium">点击率</th>
                </tr>
              </thead>
              <tbody>
                {stats.creatives.map((c) => (
                  <tr key={c.creative_id} className="border-t">
                    <td className="px-3 py-2">{c.creative_id}</td>
                    <td className="px-3 py-2">{c.name}</td>
                    <td className="px-3 py-2 text-right tabular-nums">
                      {c.impressions.toLocaleString()}
                    </td>
                    <td className="px-3 py-2 text-right tabular-nums">
                      {c.clicks.toLocaleString()}
                    </td>
                    <td className="px-3 py-2 text-right tabular-nums">
                      {pct(c.ctr)}
                    </td>
                  </tr>
                ))}
                {stats.creatives.length === 0 && (
                  <tr>
                    <td
                      colSpan={5}
                      className="px-3 py-6 text-center text-muted-foreground"
                    >
                      该区间无曝光/点击事件
                    </td>
                  </tr>
                )}
              </tbody>
              <tfoot>
                <tr className="border-t bg-muted/50 font-medium">
                  <td className="px-3 py-2" colSpan={2}>
                    {stats.totals.name}
                  </td>
                  <td className="px-3 py-2 text-right tabular-nums">
                    {stats.totals.impressions.toLocaleString()}
                  </td>
                  <td className="px-3 py-2 text-right tabular-nums">
                    {stats.totals.clicks.toLocaleString()}
                  </td>
                  <td className="px-3 py-2 text-right tabular-nums">
                    {pct(stats.totals.ctr)}
                  </td>
                </tr>
              </tfoot>
            </table>
          </div>
          <p className="text-xs text-muted-foreground">
            注：当前投放按「分数升序 ID」确定性优先（同一任务内素材分数相同、最低
            ID 素材优先），曝光可能不均衡，直接对比各素材 CTR 会受到曝光量差异影响。
          </p>
        </div>
      )}
    </div>
  );
}
