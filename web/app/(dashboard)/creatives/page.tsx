import { canWriteRole, requireMenu } from "@/lib/auth";
import {
  listCreatives,
  listAdvertisers,
  listApps,
} from "@/lib/go-api";
import { CreativesClient } from "./creatives-client";

export const dynamic = "force-dynamic"; // 管理列表实时数据

export default async function CreativesPage() {
  const session = await requireMenu("/creatives");
  const canWrite = canWriteRole(session.role);

  let creatives: Awaited<ReturnType<typeof listCreatives>> = [];
  let advertisers: Awaited<ReturnType<typeof listAdvertisers>> = [];
  let apps: Awaited<ReturnType<typeof listApps>> = [];
  let error: string | null = null;
  try {
    [creatives, advertisers, apps] = await Promise.all([
      listCreatives(session.email),
      listAdvertisers(session.email).catch(() => []),
      listApps(session.email).catch(() => []),
    ]);
  } catch (e) {
    error = e instanceof Error ? e.message : "加载失败";
  }

  return (
    <CreativesClient
      creatives={creatives}
      advertisers={advertisers}
      apps={apps}
      error={error}
      canWrite={canWrite}
    />
  );
}
