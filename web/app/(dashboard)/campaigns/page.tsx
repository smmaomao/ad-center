import Link from "next/link";
import { canWriteRole, requireMenu } from "@/lib/auth";
import {
  listCampaigns,
  listAdvertisers,
  listCreatives,
  listProducts,
} from "@/lib/go-api";
import { CampaignsClient } from "./campaigns-client";

export const dynamic = "force-dynamic"; // 管理列表实时数据

export default async function CampaignsPage() {
  const session = await requireMenu("/campaigns");
  const canWrite = canWriteRole(session.role);

  let campaigns: Awaited<ReturnType<typeof listCampaigns>> = [];
  let advertisers: Awaited<ReturnType<typeof listAdvertisers>> = [];
  let creatives: Awaited<ReturnType<typeof listCreatives>> = [];
  let products: Awaited<ReturnType<typeof listProducts>> = [];
  let error: string | null = null;
  try {
    [campaigns, advertisers, creatives, products] = await Promise.all([
      listCampaigns(session.email),
      listAdvertisers(session.email).catch(() => []),
      listCreatives(session.email).catch(() => []),
      listProducts(session.email).catch(() => []),
    ]);
  } catch (e) {
    error = e instanceof Error ? e.message : "加载失败";
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold">广告任务管理</h1>
        <Link
          href="/campaigns/analytics"
          className="text-sm text-primary hover:underline"
        >
          素材维度分析 →
        </Link>
      </div>
      <CampaignsClient
      campaigns={campaigns}
      advertisers={advertisers}
      creatives={creatives}
      products={products}
      error={error}
      canWrite={canWrite}
    />
    </div>
  );
}
