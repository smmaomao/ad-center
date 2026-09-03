import { requireRole } from "@/lib/auth";
import { Placeholder } from "@/components/placeholder";

export default async function NewSlotPage() {
  await requireRole(["super_admin", "operator"]);
  return (
    <Placeholder
      title="新增广告位"
      stage="阶段 2 实现：类型、频控、AI Agent 开关"
    />
  );
}
