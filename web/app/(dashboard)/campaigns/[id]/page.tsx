import { requireMenu, canWriteRole } from "@/lib/auth";
import {
  getCampaign,
  listAdvertisers,
  listCreatives,
  listProducts,
} from "@/lib/go-api";
import { CampaignForm } from "../campaign-form";

export const dynamic = "force-dynamic";

export default async function EditCampaignPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = await params;
  const session = await requireMenu("/campaigns");
  const canWrite = canWriteRole(session.role);

  let campaign = null;
  let error: string | null = null;
  try {
    campaign = await getCampaign(session.email, id);
  } catch (e) {
    error = e instanceof Error ? e.message : "加载失败";
  }

  if (error || !campaign) {
    return (
      <p className="text-sm text-destructive">{error ?? "任务不存在"}</p>
    );
  }
  if (!canWrite) {
    return (
      <p className="text-sm text-destructive">无权限：仅运营及以上可编辑</p>
    );
  }

  const [advertisers, creatives, products] = await Promise.all([
    listAdvertisers(session.email).catch(() => []),
    listCreatives(session.email).catch(() => []),
    listProducts(session.email).catch(() => []),
  ]);

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold">编辑广告任务 · {campaign.name}</h1>
      <CampaignForm
        advertisers={advertisers}
        creatives={creatives}
        products={products}
        initial={campaign}
      />
    </div>
  );
}
