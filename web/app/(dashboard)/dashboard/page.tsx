import { requireMenu } from "@/lib/auth";
import { PageHeader } from "@/components/page-header";

export default async function DashboardPage() {
  await requireMenu("/dashboard");

  return (
    <div className="space-y-6">
      <PageHeader
        title="实时监控"
        description="广告投放的核心指标、广告主 KPI 与广告位实时状态"
      />
      <div className="surface-card rise-1 p-10 text-center">
        <div className="mx-auto flex max-w-md flex-col items-center gap-3 text-muted-foreground">
          <div className="grid h-12 w-12 place-items-center rounded-2xl bg-brand/10 text-brand">
            <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
              <path d="M3 3v18h18" />
              <path d="m7 14 3-4 3 3 4-6" />
            </svg>
          </div>
          <p className="text-base font-semibold text-foreground">监控面板建设中</p>
          <p className="text-sm">
            阶段 3 实现（FR-08）：KPI 卡片、广告主 KPI 监控、广告位实时状态、SSE 实时刷新。
            数据源：Go 服务 <span className="font-mono text-xs">/v1/admin/metrics/stream</span>
          </p>
        </div>
      </div>
    </div>
  );
}
