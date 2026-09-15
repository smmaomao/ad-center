import { canWriteRole, requireMenu } from "@/lib/auth";
import { listAdvertisers, type AdminAdvertiser } from "@/lib/go-api";
import { AdvertisersClient } from "./advertisers-client";

export const dynamic = "force-dynamic"; // 管理列表实时数据

export default async function AdvertisersPage() {
  const session = await requireMenu("/advertisers");
  const canWrite = canWriteRole(session.role);

  let advertisers: AdminAdvertiser[] = [];
  let error: string | null = null;
  try {
    advertisers = await listAdvertisers(session.email);
  } catch (e) {
    error = e instanceof Error ? e.message : "加载失败";
  }

  return (
    <div className="space-y-6">
      <AdvertisersClient
        advertisers={advertisers}
        error={error}
        canWrite={canWrite}
      />
    </div>
  );
}
