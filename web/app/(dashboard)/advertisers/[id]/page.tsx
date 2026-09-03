import { requireRole } from "@/lib/auth";
import { Placeholder } from "@/components/placeholder";

export default async function AdvertiserDetailPage({
  params,
}: PageProps<"/advertisers/[id]">) {
  await requireRole(["super_admin", "operator"]);
  const { id } = await params;
  return (
    <Placeholder
      title={`广告主详情 ${id}`}
      stage="阶段 2 实现（FR-02）：KPI 卡片、配置、素材管理、实时数据"
    />
  );
}
