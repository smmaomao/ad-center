"use client";

// 广告主新建/编辑表单（PRD 4.1 FR-02）：基本信息、KPI 与预算、投放配置。
import { useActionState } from "react";
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

const selectCls =
  "h-8 w-full rounded-lg border border-input bg-transparent px-2 text-sm outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 disabled:opacity-50";

export function AdvertiserForm({ initial }: { initial?: AdminAdvertiser }) {
  const [state, formAction, pending] = useActionState<FormState, FormData>(
    saveAdvertiserAction,
    {},
  );

  return (
    <form action={formAction} className="space-y-6">
      {initial && <input type="hidden" name="id" value={initial.id} />}

      <Card>
        <CardHeader>
          <CardTitle>基本信息</CardTitle>
          <CardDescription>广告主身份与投放周期</CardDescription>
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
          <div className="space-y-1.5">
            <Label htmlFor="tier">Tier 分层</Label>
            <select
              id="tier"
              name="tier"
              className={selectCls}
              defaultValue={initial?.tier ?? 2}
            >
              <option value={1}>T1 · 头部（高预算优先）</option>
              <option value={2}>T2 · 腰部</option>
              <option value={3}>T3 · 长尾</option>
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
              <option value="active">投放中</option>
              <option value="paused">已暂停</option>
            </select>
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="end_at">投放结束日期</Label>
            <Input
              id="end_at"
              name="end_at"
              type="date"
              defaultValue={initial?.end_at?.slice(0, 10) ?? ""}
            />
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>KPI 与预算</CardTitle>
          <CardDescription>
            实际 CPI 逼近预算上限或 KPI 未达标时在列表页高亮预警
          </CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4 sm:grid-cols-3">
          <div className="space-y-1.5">
            <Label htmlFor="target_cpi">目标 CPI ($)</Label>
            <Input
              id="target_cpi"
              name="target_cpi"
              type="number"
              step="0.01"
              min="0"
              defaultValue={initial?.target_cpi || ""}
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="daily_budget">日预算 ($)</Label>
            <Input
              id="daily_budget"
              name="daily_budget"
              type="number"
              step="0.01"
              min="0"
              defaultValue={initial?.daily_budget || ""}
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="consume_speed">消耗节奏</Label>
            <select
              id="consume_speed"
              name="consume_speed"
              className={selectCls}
              defaultValue={initial?.consume_speed ?? "even"}
            >
              <option value="even">均匀消耗</option>
              <option value="accelerated">加速消耗</option>
              <option value="asap">尽快花完</option>
            </select>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>投放配置</CardTitle>
          <CardDescription>计费模式与保量份额（保量份额用于保量广告位）</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4 sm:grid-cols-2">
          <div className="space-y-1.5">
            <Label htmlFor="bidding_mode">计费模式</Label>
            <select
              id="bidding_mode"
              name="bidding_mode"
              className={selectCls}
              defaultValue={initial?.bidding_mode ?? "cpi"}
            >
              <option value="cpi">CPI · 按安装</option>
              <option value="cpa">CPA · 按行为</option>
              <option value="revenue_share">Revenue Share · 收入分成</option>
            </select>
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="bidding_price">出价 ($)</Label>
            <Input
              id="bidding_price"
              name="bidding_price"
              type="number"
              step="0.01"
              min="0"
              defaultValue={initial?.bidding_price || ""}
            />
          </div>
          <label className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              name="guaranteed_enabled"
              defaultChecked={initial?.guaranteed_enabled ?? false}
              className="size-4"
            />
            启用保量
          </label>
          <div className="space-y-1.5">
            <Label htmlFor="guaranteed_min_share">保量最小份额 (0~1)</Label>
            <Input
              id="guaranteed_min_share"
              name="guaranteed_min_share"
              type="number"
              step="0.01"
              min="0"
              max="1"
              defaultValue={initial?.guaranteed_min_share || ""}
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
