import { requireRole } from "@/lib/auth";
import {
  getSlot,
  listAdvertisers,
  listApps,
  type AdminAdvertiser,
} from "@/lib/go-api";
import { SlotForm } from "../slot-form";
import { PriorityEditor } from "../priority-editor";
import { DeleteEntityButton } from "@/components/danger-zone";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";

export const dynamic = "force-dynamic";

export default async function SlotDetailPage({
  params,
}: PageProps<"/slots/[id]">) {
  const session = await requireRole(["super_admin", "operator"]);
  const { id } = await params;

  let loadError: string | null = null;
  let slot: Awaited<ReturnType<typeof getSlot>> | null = null;
  let advertisers: AdminAdvertiser[] = [];
  try {
    [slot, advertisers] = await Promise.all([
      getSlot(session.email, id),
      listAdvertisers(session.email),
    ]);
  } catch (e) {
    loadError = e instanceof Error ? e.message : "加载失败";
  }
  // 表单需要 App 下拉（编辑态禁用但仍需渲染选项）
  const apps = await listApps(session.email).catch(() => []);

  if (loadError || !slot) {
    return (
      <Card className="border-destructive">
        <CardHeader>
          <CardTitle className="text-destructive">加载失败</CardTitle>
        </CardHeader>
        <CardContent>{loadError ?? "广告位不存在"}</CardContent>
      </Card>
    );
  }

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between">
        <div>
          <h1 className="flex items-center gap-2 text-2xl font-semibold">
            {slot.name}
            <span
              className={`rounded-full px-2 py-0.5 text-xs font-medium ${
                slot.status === "active"
                  ? "bg-emerald-50 text-emerald-700"
                  : "bg-zinc-100 text-zinc-600"
              }`}
            >
              {slot.status === "active" ? "启用" : "停用"}
            </span>
            {slot.ai_agent_enabled && (
              <span className="rounded-full bg-violet-50 px-2 py-0.5 text-xs font-medium text-violet-700">
                AI Agent 已接管
              </span>
            )}
          </h1>
          <p className="font-mono text-sm text-muted-foreground">
            {slot.app_name} / {slot.key}
          </p>
        </div>
        {session.role === "super_admin" && (
          <DeleteEntityButton kind="slot" id={id} label={slot.name} />
        )}
      </div>

      <SlotForm apps={apps} initial={slot} />

      <PriorityEditor
        slotId={id}
        initial={slot.priorities ?? []}
        advertisers={advertisers.map((a) => ({ id: a.id, name: a.name }))}
      />
    </div>
  );
}
