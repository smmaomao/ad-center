import { requireMenu, canWriteRole } from "@/lib/auth";
import { listAdvertisers, listCreatives } from "@/lib/go-api";
import { CampaignForm } from "../campaign-form";

export const dynamic = "force-dynamic";

export default async function NewCampaignPage() {
  const session = await requireMenu("/campaigns");
  if (!canWriteRole(session.role)) {
    return (
      <p className="text-sm text-destructive">无权限：仅运营及以上可创建任务</p>
    );
  }

  const [advertisers, creatives] = await Promise.all([
    listAdvertisers(session.email).catch(() => []),
    listCreatives(session.email).catch(() => []),
  ]);

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold">新建广告任务</h1>
      <CampaignForm advertisers={advertisers} creatives={creatives} />
    </div>
  );
}
