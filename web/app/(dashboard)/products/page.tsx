import { canWriteRole, requireMenu } from "@/lib/auth";
import {
  listProducts,
  listAdvertisers,
  listCampaigns,
  type AdminProduct,
  type AdminAdvertiser,
  type AdminCampaign,
} from "@/lib/go-api";
import { ProductsClient } from "./products-client";

export const dynamic = "force-dynamic"; // 管理列表实时数据

export default async function ProductsPage({
  searchParams,
}: {
  searchParams: Promise<{ advertiser_id?: string }>;
}) {
  const session = await requireMenu("/products");
  const canWrite = canWriteRole(session.role);
  const { advertiser_id } = await searchParams;

  let products: AdminProduct[] = [];
  let advertisers: AdminAdvertiser[] = [];
  let campaigns: AdminCampaign[] = [];
  let error: string | null = null;
  try {
    [products, advertisers, campaigns] = await Promise.all([
      listProducts(session.email, advertiser_id),
      listAdvertisers(session.email).catch(() => []),
      listCampaigns(session.email).catch(() => []),
    ]);
  } catch (e) {
    error = e instanceof Error ? e.message : "加载失败";
  }

  // 每个产品的任务汇总（campaign 为 KPI 执行粒度，product 是归集层）
  const productRollup: Record<
    string,
    { count: number; budget: number; spent: number; guaranteed: number }
  > = {};
  for (const c of campaigns) {
    if (!c.product_id) continue;
    const r =
      productRollup[c.product_id] ??
      (productRollup[c.product_id] = {
        count: 0,
        budget: 0,
        spent: 0,
        guaranteed: 0,
      });
    r.count += 1;
    r.budget += c.daily_budget || 0;
    r.spent += c.spent_today || 0;
    r.guaranteed = Math.max(r.guaranteed, c.guaranteed_min_share || 0);
  }

  const filterAdvertiser = advertiser_id
    ? (advertisers.find((a) => a.id === advertiser_id)?.name ?? null)
    : null;

  return (
    <ProductsClient
      products={products}
      advertisers={advertisers}
      rollups={productRollup}
      error={error}
      canWrite={canWrite}
      filterAdvertiser={filterAdvertiser}
    />
  );
}
