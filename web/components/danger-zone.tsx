"use client";

// 危险操作按钮：确认后调软删 Server Action（Go 侧仅 super_admin 放行）。
import { useTransition } from "react";
import { deleteAdvertiserAction } from "@/app/(dashboard)/advertisers/actions";
import { deleteSlotAction } from "@/app/(dashboard)/slots/actions";
import { Button } from "@/components/ui/button";

export function DeleteEntityButton({
  kind,
  id,
  label,
}: {
  kind: "advertiser" | "slot";
  id: string;
  label: string;
}) {
  const [pending, startTransition] = useTransition();

  function onClick() {
    if (!confirm(`确认删除「${label}」？删除后立即生效，历史数据保留。`)) return;
    startTransition(async () => {
      const run =
        kind === "advertiser"
          ? () => deleteAdvertiserAction(id)
          : () => deleteSlotAction(id);
      const res = await run();
      if (res?.error) alert(res.error);
    });
  }

  return (
    <Button variant="destructive" onClick={onClick} disabled={pending}>
      {pending ? "删除中…" : `删除${kind === "advertiser" ? "广告主" : "广告位"}`}
    </Button>
  );
}
