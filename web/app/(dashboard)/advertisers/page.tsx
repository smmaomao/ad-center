import { requireRole } from "@/lib/auth";
import { Placeholder } from "@/components/placeholder";

export default async function AdvertisersPage() {
  await requireRole(["super_admin", "operator"]);
  return (
    <Placeholder
      title="广告主管理"
      stage="阶段 2 实现（FR-01）：列表、筛选、状态预警、KPI 达成率标识"
    />
  );
}
