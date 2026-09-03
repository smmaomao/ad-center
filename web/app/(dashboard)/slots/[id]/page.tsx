import { requireRole } from "@/lib/auth";
import { Placeholder } from "@/components/placeholder";

export default async function SlotDetailPage({
  params,
}: PageProps<"/slots/[id]">) {
  await requireRole(["super_admin", "operator"]);
  const { id } = await params;
  return (
    <Placeholder
      title={`广告位策略配置 ${id}`}
      stage="阶段 2 实现（FR-04）：填充优先级拖拽排序、高级策略、AI Agent 接管"
    />
  );
}
