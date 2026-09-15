"use client";

// 平台计费标准线（系统设置）：打分引擎的 100 分基准。
// 素材出价基准分 = 素材实际出价 ÷ 对应扣费模式的标准线 × 100
import { useActionState } from "react";
import type { PricingBenchmark } from "@/lib/go-api";
import { updatePricingBenchmarkAction, type FormState } from "./actions";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

const FIELDS: {
  key: keyof PricingBenchmark;
  label: string;
  hint?: string;
}[] = [
  { key: "cpm", label: "CPM 标准线（元 / 千次曝光）", hint: "出价 15 → 基准分 100" },
  { key: "cpc", label: "CPC 标准线（元 / 点击）", hint: "出价 6 → 120 分" },
  { key: "cpa_install", label: "CPA 安装标准线（元 / 个）" },
  { key: "cpa_activate", label: "CPA 激活标准线（元 / 个）" },
  { key: "cpa_register", label: "CPA 注册标准线（元 / 个）" },
  {
    key: "cpa_first_purchase",
    label: "CPA 首充标准线（元 / 个）",
    hint: "出价 10 → 50 分",
  },
];

export function PricingBenchmarkForm({
  initial,
}: {
  initial: PricingBenchmark;
}) {
  const [state, formAction, pending] = useActionState<FormState, FormData>(
    updatePricingBenchmarkAction,
    {},
  );

  return (
    <form action={formAction} className="space-y-4">
      <p className="text-sm text-muted-foreground">
        改这里等于改整个打分引擎的 100 分基准；保存后配置热更新生效，无需重启。
        标准线调高 → 所有素材的分被压低；调低 → 分被抬高。
      </p>

      <div className="grid gap-4 sm:grid-cols-2">
        {FIELDS.map((f) => (
          <div key={f.key} className="space-y-1.5">
            <Label htmlFor={f.key}>{f.label}</Label>
            <Input
              id={f.key}
              name={f.key}
              type="number"
              step="0.01"
              min="0.01"
              required
              defaultValue={initial[f.key]}
              className="max-w-[12rem]"
            />
            {f.hint && (
              <p className="text-xs text-muted-foreground">{f.hint}</p>
            )}
          </div>
        ))}
      </div>

      {state.error && (
        <p className="text-sm font-medium text-destructive">{state.error}</p>
      )}
      {state.ok && (
        <p className="text-sm font-medium text-green-600">已保存</p>
      )}
      <Button type="submit" disabled={pending}>
        {pending ? "保存中…" : "保存标准线"}
      </Button>
    </form>
  );
}
