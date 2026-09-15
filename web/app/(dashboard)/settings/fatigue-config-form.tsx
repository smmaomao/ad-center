"use client";

import { useActionState } from "react";
import {
  updateFatigueConfigAction,
  type FormState,
} from "./actions";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

export function FatigueConfigForm({
  initial,
}: {
  initial: {
    enabled: boolean;
    window_minutes: number;
    window_max: number;
    daily_max: number;
  };
}) {
  const [state, formAction, pending] = useActionState<FormState, FormData>(
    updateFatigueConfigAction,
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
          启用全局疲劳度控制
        </label>
        <p className="text-xs text-muted-foreground">
          开启后，按「用户 × 素材」对观看次数做双重限流，超出上限的素材在对应
          窗口结束前对该用户自动隐藏。
        </p>
      </div>

      <div className="grid gap-4 sm:grid-cols-3">
        <div className="space-y-1.5">
          <Label htmlFor="window_minutes">时间窗（分钟）</Label>
          <Input
            id="window_minutes"
            name="window_minutes"
            type="number"
            min={1}
            max={1440}
            step={1}
            defaultValue={initial.window_minutes}
            className="max-w-[10rem]"
          />
          <p className="text-xs text-muted-foreground">
            控制 1 的时间窗长度（1~1440）。从首次观看起算整段时长，非滑动窗口。
          </p>
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="window_max">窗口内上限</Label>
          <Input
            id="window_max"
            name="window_max"
            type="number"
            min={1}
            step={1}
            defaultValue={initial.window_max}
            className="max-w-[10rem]"
          />
          <p className="text-xs text-muted-foreground">
            控制 1：该时间窗内同一用户最多观看同一素材的次数。
          </p>
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="daily_max">每日上限</Label>
          <Input
            id="daily_max"
            name="daily_max"
            type="number"
            min={1}
            step={1}
            defaultValue={initial.daily_max}
            className="max-w-[10rem]"
          />
          <p className="text-xs text-muted-foreground">
            控制 2：每日（滚动 24h）同一用户最多观看同一素材的次数。
          </p>
        </div>
      </div>

      <p className="text-xs text-muted-foreground">
        计数发生在用户真实观看（impression）时，与决策缓存解耦；多实例共享同一
        Redis（本项目独占一个 DB index 与其他项目隔离），key 形如
        user:ad:limit:{"{user}"}+{"{creative}"} 与
        user:ad:daily:{"{user}"}+{"{creative}"}。
      </p>

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
