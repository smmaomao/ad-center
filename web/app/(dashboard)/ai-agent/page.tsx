import { requireRole } from "@/lib/auth";
import { Placeholder } from "@/components/placeholder";

export default async function AiAgentPage() {
  await requireRole(["super_admin", "operator", "analyst", "strategy"]);
  return (
    <Placeholder
      title="AI Agent 控制台"
      stage="P1 实现（FR-05/06/07）：总览仪表盘、决策日志、策略配置"
    />
  );
}
