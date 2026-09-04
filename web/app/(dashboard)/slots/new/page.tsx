import { requireRole } from "@/lib/auth";
import { listApps, type AdminApp } from "@/lib/go-api";
import { SlotForm } from "../slot-form";

export const dynamic = "force-dynamic";

export default async function NewSlotPage() {
  const session = await requireRole(["super_admin", "operator"]);
  const apps: AdminApp[] = await listApps(session.email).catch(() => []);

  return (
    <div className="mx-auto max-w-3xl space-y-6">
      <div>
        <h1 className="text-2xl font-semibold">新增广告位</h1>
        <p className="text-sm text-muted-foreground">
          创建后配置填充优先级与保量策略
        </p>
      </div>
      <SlotForm apps={apps} />
    </div>
  );
}
