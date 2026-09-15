"use client";

import { useActionState } from "react";
import { updateDecisionCacheAction, type FormState } from "./actions";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

export function DecisionCacheForm({
  initial,
}: {
  initial: { enabled: boolean; ttl_seconds: number };
}) {
  const [state, formAction, pending] = useActionState<FormState, FormData>(
    updateDecisionCacheAction,
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
          启用决策结果缓存
        </label>
        <p className="text-xs text-muted-foreground">
          开启后，相同（App/广告位/设备/定向/条数）的请求在缓存时长内复用同一决策，
          降低引擎计算与预算/频控读取压力；窗口内预算/频控允许少量超发。
        </p>
      </div>
      <div className="space-y-1.5">
        <Label htmlFor="ttl_seconds">缓存时长（秒，1~3600）</Label>
        <Input
          id="ttl_seconds"
          name="ttl_seconds"
          type="number"
          min={1}
          max={3600}
          step={1}
          defaultValue={initial.ttl_seconds}
          className="max-w-[12rem]"
        />
        <p className="text-xs text-muted-foreground">
          建议 300（5 分钟）~ 600（10 分钟）。过长会导致配置变更/预算重置生效延迟。
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
