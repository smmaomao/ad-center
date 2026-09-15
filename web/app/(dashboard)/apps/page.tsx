import { requireMenu, canWriteRole } from "@/lib/auth";
import { listApps } from "@/lib/go-api";
import { AppsClient } from "./apps-client";

export const dynamic = "force-dynamic";

export default async function AppsPage() {
  const session = await requireMenu("/apps");
  const canWrite = canWriteRole(session.role);
  const apps = await listApps(session.email).catch(() => []);
  return <AppsClient apps={apps} canWrite={canWrite} />;
}
