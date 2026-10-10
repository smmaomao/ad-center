"use client";

import { useActionState } from "react";
import { updateLoopableAction, type FormState } from "../actions";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";

export function LoopableForm({
  initial,
}: {
  initial: { enabled: boolean };
}) {
  const [state, formAction, pending] = useActionState<FormState, FormData>(
    updateLoopableAction,
    {},
  );
  return (
    <form action={formAction} className="space-y-6">
      <div className="space-y-1.5">
        <label className="flex items-center gap-2 text-sm">
          <input
            type="checkbox"
            name="enabled"
            defaultChecked={initial.enabled}
            className="size-4"
          />
          允许广告列表循环播放
        </label>
        <p className="text-xs text-muted-foreground">
          开启后，整个广告列表在客户端缓存期内可循环播放；关闭则禁止循环。
          开关立即生效（配置快照秒级热更新），无需重启服务。
        </p>
      </div>

      {state.error && (
        <p className="text-sm font-medium text-destructive">{state.error}</p>
      )}
      {state.ok && <p className="text-sm font-medium text-green-600">已保存</p>}
      <div className="flex gap-2">
        <Button type="submit" disabled={pending}>
          {pending ? "保存中…" : "保存"}
        </Button>
      </div>
    </form>
  );
}
