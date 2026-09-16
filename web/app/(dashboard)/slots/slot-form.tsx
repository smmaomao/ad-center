"use client";

// 广告位新建/编辑表单（PRD FR-03）：类型、频控、AI Agent 开关。
import { useActionState } from "react";
import type { AdminApp, AdminSlot } from "@/lib/go-api";
import { createSlotAction, updateSlotAction, type FormState } from "./actions";
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

export const SLOT_TYPE_LABEL: Record<string, string> = {
  rewarded_video: "激励视频",
  splash: "开屏",
  interstitial: "插屏",
  feed: "信息流",
  banner: "Banner",
};

export function SlotForm({
  apps,
  initial,
}: {
  apps: AdminApp[];
  initial?: AdminSlot;
}) {
  const [state, formAction, pending] = useActionState<FormState, FormData>(
    initial ? updateSlotAction : createSlotAction,
    {},
  );
  const isEdit = Boolean(initial);

  return (
    <form action={formAction} className="space-y-6">
      {initial && <input type="hidden" name="id" value={initial.id} />}

      <Card>
        <CardHeader>
          <CardTitle>基本信息</CardTitle>
          {!isEdit && (
            <CardDescription>
              slot_key 是客户端请求的稳定标识，创建后不可修改
            </CardDescription>
          )}
        </CardHeader>
        <CardContent className="grid gap-4 sm:grid-cols-2">
          <div className="space-y-1.5">
            <Label htmlFor="app_id">所属 App *</Label>
            <select
              id="app_id"
              name="app_id"
              className={selectCls}
              defaultValue={initial?.app_id ?? ""}
              required
              disabled={isEdit}
            >
              <option value="" disabled>
                选择 App
              </option>
              {apps.map((a) => (
                <option key={a.id} value={a.id}>
                  {a.name}
                </option>
              ))}
            </select>
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="slot_key">Slot Key *</Label>
            <Input
              id="slot_key"
              name="slot_key"
              placeholder="如 game_reward_video"
              pattern="[a-z0-9_]{2,64}"
              required
              defaultValue={initial?.key}
              disabled={isEdit}
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="name">展示名称 *</Label>
            <Input id="name" name="name" required defaultValue={initial?.name} />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="type">广告位类型 *</Label>
            <select
              id="type"
              name="type"
              className={selectCls}
              defaultValue={initial?.type ?? "rewarded_video"}
            >
              {Object.entries(SLOT_TYPE_LABEL).map(([v, label]) => (
                <option key={v} value={v}>
                  {label}
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
              <option value="paused">停用</option>
            </select>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>频次控制</CardTitle>
          <CardDescription>
            0 表示不限制；疲劳度窗口内达到日上限后返回兜底
          </CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4 sm:grid-cols-3">
          <div className="space-y-1.5">
            <Label htmlFor="freq_daily_limit">单用户日上限（次）</Label>
            <Input
              id="freq_daily_limit"
              name="freq_daily_limit"
              type="number"
              min="0"
              defaultValue={initial?.freq_daily_limit || ""}
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="freq_interval_minutes">展示间隔（分钟）</Label>
            <Input
              id="freq_interval_minutes"
              name="freq_interval_minutes"
              type="number"
              min="0"
              defaultValue={initial?.freq_interval_minutes || ""}
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="freq_fatigue_window">疲劳度窗口（分钟）</Label>
            <Input
              id="freq_fatigue_window"
              name="freq_fatigue_window"
              type="number"
              min="0"
              defaultValue={initial?.freq_fatigue_window || ""}
            />
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>AI Agent 接管</CardTitle>
          <CardDescription>
            开启后该广告位的填充决策由 AI Agent 按目标自动调优
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <label className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              name="ai_agent_enabled"
              defaultChecked={initial?.ai_agent_enabled ?? false}
              className="size-4"
            />
            启用 AI Agent 接管
          </label>
          <div className="space-y-1.5">
            <Label htmlFor="ai_agent_goal">优化目标</Label>
            <select
              id="ai_agent_goal"
              name="ai_agent_goal"
              className={selectCls}
              defaultValue={initial?.ai_agent_goal || "kpi_first"}
            >
              <option value="kpi_first">KPI 优先</option>
              <option value="budget_smooth">预算平滑</option>
              <option value="ecpm_max">eCPM 最大化</option>
            </select>
          </div>
        </CardContent>
      </Card>

      {state.error && (
        <p className="text-sm font-medium text-destructive">{state.error}</p>
      )}
      <Button type="submit" disabled={pending}>
        {pending ? "保存中…" : isEdit ? "保存修改" : "创建广告位"}
      </Button>
    </form>
  );
}
