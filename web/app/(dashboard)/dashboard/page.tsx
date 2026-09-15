import { requireMenu } from "@/lib/auth";
import { PageHeader } from "@/components/page-header";
import { DashboardLive } from "./dashboard-live";

export default async function DashboardPage() {
  await requireMenu("/dashboard");

  return (
    <div className="space-y-6">
      <PageHeader
        title="实时监控"
        description="广告投放的核心指标、广告主 KPI 与广告位实时状态"
      />
      <DashboardLive />
    </div>
  );
}
