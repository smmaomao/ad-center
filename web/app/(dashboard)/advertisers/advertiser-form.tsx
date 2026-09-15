"use client";

// 广告主新建/编辑表单：仅承载「身份 + 计费锚点」。
// KPI / 排期 / 消耗节奏 / 下发有效期 / 计费方式 / 保量份额 均在广告任务（campaign）维度配置。
import { useActionState, useEffect } from "react";
import type { AdminAdvertiser } from "@/lib/go-api";
import { saveAdvertiserAction, type FormState } from "./actions";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

export function AdvertiserForm({
  initial,
  modal,
  onSaved,
}: {
  initial?: AdminAdvertiser;
  modal?: boolean;
  onSaved?: () => void;
}) {
  const [state, formAction, pending] = useActionState<FormState, FormData>(
    saveAdvertiserAction,
    {},
  );

  useEffect(() => {
    if (state.ok) onSaved?.();
  }, [state, onSaved]);

  return (
    <form action={formAction} className="space-y-6">
      {initial && <input type="hidden" name="id" value={initial.id} />}
      {modal && <input type="hidden" name="no_redirect" value="1" />}

      <Card>
        <CardHeader>
          <CardTitle>基本信息</CardTitle>
          <CardDescription>广告主身份（KPI / 排期在广告任务维度配置）</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4 sm:grid-cols-2">
          <div className="space-y-1.5">
            <Label htmlFor="name">广告主名称 *</Label>
            <Input id="name" name="name" required defaultValue={initial?.name} />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="contact">联系方式</Label>
            <Input
              id="contact"
              name="contact"
              placeholder="邮箱 / Telegram"
              defaultValue={initial?.contact}
            />
          </div>
          <div className="space-y-1.5 sm:col-span-2">
            <Label htmlFor="notes">备注</Label>
            <Input
              id="notes"
              name="notes"
              placeholder="内部备注（可选）"
              defaultValue={initial?.notes}
            />
          </div>
        </CardContent>
      </Card>

      {state.error && (
        <p className="text-sm font-medium text-destructive">{state.error}</p>
      )}
      <div className="flex gap-2">
        <Button type="submit" disabled={pending}>
          {pending ? "保存中…" : initial ? "保存修改" : "创建广告主"}
        </Button>
      </div>
    </form>
  );
}
