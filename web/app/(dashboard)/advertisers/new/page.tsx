import { requireRole } from "@/lib/auth";
import { Placeholder } from "@/components/placeholder";

export default async function NewAdvertiserPage() {
  await requireRole(["super_admin", "operator"]);
  return (
    <Placeholder
      title="新增广告主"
      stage="阶段 2 实现（FR-02）：基本信息、KPI 与预算、投放配置"
    />
  );
}
