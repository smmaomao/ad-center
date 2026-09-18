import { requireMenu } from "@/lib/auth";
import { PageHeader } from "@/components/page-header";
import { DashboardLive } from "./dashboard-live";

export default async function DashboardPage() {
  await requireMenu("/dashboard");

  return (
    <div className="space-y-6">
      <PageHeader
        title={
          <span>
            实时监控
            <span className="ml-3 align-middle text-xs font-normal text-red-500">
              当前功能未完善，请不要参考
            </span>
          </span>
        }
        description="广告投放的核心指标、广告主 KPI 与广告位实时状态"
      />
      <DashboardLive />
    </div>
  );
}
