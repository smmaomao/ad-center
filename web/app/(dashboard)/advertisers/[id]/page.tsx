import Link from "next/link";
import { requireMenu, canWriteRole } from "@/lib/auth";
import {
  getAdvertiser,
  listProducts,
  getAdvertiserRollup,
  getWallet,
  listWalletFlow,
  type AdminAdvertiser,
  type AdminProduct,
  type AdminWallet,
  type CampaignRollup,
  type WalletFlowRow,
} from "@/lib/go-api";
import { DeleteEntityButton } from "@/components/danger-zone";
import { WalletPanel } from "./wallet-panel";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

export const dynamic = "force-dynamic"; // KPI/消耗实时数据

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
  const session = await requireMenu("/advertisers");
  const { id } = await params;

  let advertiser: AdminAdvertiser | null = null;
  let loadError: string | null = null;
  try {
    advertiser = await getAdvertiser(session.email, id);
  } catch (e) {
    loadError = e instanceof Error ? e.message : "加载失败";
  }
  let products: AdminProduct[] = [];
  try {
    products = await listProducts(session.email, id);
  } catch {
    products = [];
  }
  let rollup: CampaignRollup | null = null;
  try {
    rollup = await getAdvertiserRollup(session.email, id);
  } catch {
    rollup = null;
  }
  let wallet: AdminWallet | null = null;
  try {
    wallet = await getWallet(session.email, id);
  } catch {
    wallet = null;
  }
  let walletFlow: WalletFlowRow[] = [];
  try {
    walletFlow = await listWalletFlow(session.email, id);
  } catch {
    walletFlow = [];
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

  const spent = rollup?.spent_today ?? 0;

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="flex items-center gap-2 text-2xl font-semibold">
            {advertiser.name}
          </h1>
          <p className="text-sm text-muted-foreground">
            {advertiser.contact || "未填写联系方式"}
          </p>
        </div>
        {session.role === "super_admin" && (
          <DeleteEntityButton kind="advertiser" id={id} label={advertiser.name} />
        )}
      </div>

      {/* KPI 卡片：按旗下任务汇总（campaign 为 KPI 执行粒度，advertiser 是归集层） */}
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {kpiCard(
          "今日消耗（旗下任务汇总）",
          `$${spent.toFixed(2)}`,
          "预算在各广告任务中分别设置",
        )}
        {kpiCard(
          "KPI 达成率（任务汇总）",
          `${((rollup?.achievement ?? 1) * 100).toFixed(0)}%`,
          `实际 CPI $${(rollup?.actual_cpi ?? 0).toFixed(2)} / 目标 $${(rollup?.target_cpi ?? 0).toFixed(2)}`,
          (rollup?.achievement ?? 1) < 0.85,
        )}
        {kpiCard(
          "保量份额（任务最大）",
          rollup && rollup.guaranteed_min_share > 0
            ? `${(rollup.guaranteed_min_share * 100).toFixed(0)}%`
            : "未启用",
          "旗下任务中最大的保量填充份额",
        )}
        {kpiCard(
          "旗下任务",
          `${rollup?.campaign_count ?? 0} 个`,
          "KPI / 预算的细化在广告任务管理",
        )}
      </div>

      {/* 账户总钱包：充值 - 扣费；余额为投放硬顶（日预算没超、余额超了也停投） */}
      {wallet && (
        <WalletPanel
          advertiserId={id}
          wallet={wallet}
          flow={walletFlow}
          canWrite={canWriteRole(session.role)}
        />
      )}

      <div className="surface-card rise-1 p-6 text-sm text-muted-foreground">
        素材与投放配置不属于单个广告主：素材由「
        <span className="font-medium text-foreground">素材管理</span>」公共库统一维护，
        再在「<span className="font-medium text-foreground">广告任务管理</span>」中按需绑定到任务。
      </div>

      {/* 旗下产品（多产品广告主）：从广告主视角看清产品结构与归集 */}
      <Card>
        <CardHeader>
          <div className="flex items-center justify-between gap-2">
            <div>
              <CardTitle>旗下产品</CardTitle>
              <CardDescription>
                该广告主下所有产品，点击产品名跳转到产品管理并按本广告主筛选
              </CardDescription>
            </div>
            <Link
              href="/products"
              className="shrink-0 text-sm text-brand hover:underline"
            >
              全部产品 →
            </Link>
          </div>
        </CardHeader>
        <CardContent>
          {products.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              该广告主下还没有产品。可在「产品管理」中新增产品，建立多产品结构。
            </p>
          ) : (
            <ul className="divide-y divide-border">
              {products.map((p) => (
                <li
                  key={p.id}
                  className="flex items-center justify-between gap-4 py-3"
                >
                  <div className="min-w-0">
                    <Link
                      href={`/products?advertiser_id=${advertiser.id}`}
                      className="font-medium text-foreground hover:text-brand hover:underline"
                    >
                      {p.name}
                    </Link>
                    {p.notes && (
                      <p className="truncate text-xs text-muted-foreground">
                        {p.notes}
                      </p>
                    )}
                  </div>
                  <div className="flex shrink-0 items-center gap-4 text-sm">
                    <span className="tabular-nums text-muted-foreground">
                      {p.daily_budget ? `$${p.daily_budget}` : "不限"}
                    </span>
                    <span
                      className={`rounded-full px-2 py-0.5 text-xs font-medium ${
                        p.status === "active"
                          ? "bg-emerald-50 text-emerald-700"
                          : "bg-zinc-100 text-zinc-600"
                      }`}
                    >
                      {p.status === "active" ? "启用" : "已暂停"}
                    </span>
                  </div>
                </li>
              ))}
            </ul>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
