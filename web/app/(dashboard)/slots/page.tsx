import { requireRole } from "@/lib/auth";
import { Placeholder } from "@/components/placeholder";

export default async function SlotsPage() {
  await requireRole(["super_admin", "operator"]);
  return (
    <Placeholder
      title="广告位管理"
      stage="阶段 2 实现（FR-03）：列表、填充状态、筛选"
    />
  );
}
