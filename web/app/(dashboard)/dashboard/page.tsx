import { requireRole } from "@/lib/auth";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

export default async function DashboardPage() {
  await requireRole(["super_admin", "operator", "analyst", "strategy"]);

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold">实时监控</h1>
      <Card>
        <CardHeader>
          <CardTitle>建设中</CardTitle>
          <CardDescription>
            阶段 3 实现（FR-08）：KPI 卡片、广告主 KPI 监控、广告位实时状态、SSE 实时刷新
          </CardDescription>
        </CardHeader>
        <CardContent className="text-sm text-muted-foreground">
          数据源：Go 服务 /v1/admin/metrics/stream
        </CardContent>
      </Card>
    </div>
  );
}
