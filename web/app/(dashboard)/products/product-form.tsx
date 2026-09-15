"use client";

// 产品（Product）表单：基础信息 + 状态 + 日预算 + 备注
import { useActionState, useEffect } from "react";
import type { AdminAdvertiser, AdminProduct } from "@/lib/go-api";
import { saveProductAction, type ProductFormState } from "./actions";
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

const selectCls =
  "h-8 w-full rounded-lg border border-input bg-transparent px-2 text-sm outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 disabled:opacity-50";

export function ProductForm({
  advertisers,
  initial,
  modal,
  onSaved,
}: {
  advertisers: AdminAdvertiser[];
  initial?: AdminProduct;
  modal?: boolean;
  onSaved?: () => void;
}) {
  const [state, formAction, pending] = useActionState<
    ProductFormState,
    FormData
  >(saveProductAction, {});
  const isEdit = Boolean(initial);

  // 弹窗内保存成功后，通知外层关闭弹窗
  useEffect(() => {
    if (state.ok) onSaved?.();
  }, [state, onSaved]);

  return (
    <form action={formAction} className="space-y-6">
      {initial && <input type="hidden" name="id" value={initial.id} />}
      {modal && <input type="hidden" name="no_redirect" value="1" />}

      <Card>
        <CardHeader>
          <CardTitle>基础信息</CardTitle>
          <CardDescription>产品归属广告主与状态</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4 sm:grid-cols-2">
          <div className="space-y-1.5">
            <Label htmlFor="name">产品名称 *</Label>
            <Input id="name" name="name" required defaultValue={initial?.name} />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="advertiser_id">所属广告主 *</Label>
            <select
              id="advertiser_id"
              name="advertiser_id"
              className={selectCls}
              defaultValue={initial?.advertiser_id ?? ""}
              required
            >
              <option value="" disabled>
                选择广告主
              </option>
              {advertisers.map((a) => (
                <option key={a.id} value={a.id}>
                  {a.name}
                </option>
              ))}
            </select>
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="status">状态</Label>
            <select
              id="status"
              name="status"
              className={selectCls}
              defaultValue={initial?.status ?? "active"}
            >
              <option value="active">启用</option>
              <option value="paused">已暂停</option>
            </select>
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="daily_budget">产品日预算（0 = 不限）</Label>
            <Input
              id="daily_budget"
              name="daily_budget"
              type="number"
              step="0.01"
              min="0"
              defaultValue={initial?.daily_budget || ""}
            />
          </div>
          <div className="space-y-1.5 sm:col-span-2">
            <Label htmlFor="notes">备注</Label>
            <Input id="notes" name="notes" defaultValue={initial?.notes ?? ""} />
          </div>
        </CardContent>
      </Card>

      {state.error && (
        <p className="text-sm font-medium text-destructive">{state.error}</p>
      )}
      {state.ok && (
        <p className="text-sm font-medium text-green-600">已保存</p>
      )}
      <Button type="submit" disabled={pending}>
        {pending ? "保存中…" : isEdit ? "保存修改" : "创建产品"}
      </Button>
    </form>
  );
}
