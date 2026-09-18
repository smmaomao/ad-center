"use client";

// 广告任务（Campaign）表单：出价 / KPI / 单价 / 频控 + 关联公共素材库
import { useActionState, useEffect, useState } from "react";
import type {
  AdminAdvertiser,
  AdminCreative,
  AdminCampaign,
  AdminProduct,
} from "@/lib/go-api";
import type { BillingMode } from "@/lib/go-api";
import { saveCampaignAction, type FormState } from "./actions";
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

export function CampaignForm({
  advertisers,
  creatives,
  products,
  initial,
  modal,
  onSaved,
}: {
  advertisers: AdminAdvertiser[];
  creatives: AdminCreative[];
  products?: AdminProduct[];
  initial?: AdminCampaign;
  modal?: boolean;
  onSaved?: () => void;
}) {
  const [state, formAction, pending] = useActionState<FormState, FormData>(
    saveCampaignAction,
    {},
  );
  // 扣费方式 / KPI 指标类型共用选项（两者枚举一致）
  const billingModeOptions = [
    { value: "cpm", label: "CPM · 千次曝光" },
    { value: "cpc", label: "CPC · 点击" },
    { value: "cpi", label: "CPI · 安装" },
    { value: "cpa-activate", label: "CPA-激活" },
    { value: "cpa-register", label: "CPA-注册" },
    { value: "cpa-first-deposit", label: "CPA-首充" },
    { value: "cpa-pay", label: "CPA-付费" },
  ] as const;
  const [billingMode, setBillingMode] = useState<BillingMode>(
    initial?.billing_mode ?? "cpm",
  );
  const isEdit = Boolean(initial);
  const [advertiserId, setAdvertiserId] = useState<string>(
    initial?.advertiser_id ?? "",
  );

  // 弹窗内保存成功后，通知外层关闭弹窗
  useEffect(() => {
    if (state.ok) onSaved?.();
  }, [state, onSaved]);

  return (
    <form action={formAction} className="space-y-6">
      {initial && <input type="hidden" name="id" value={initial.id} />}
      {modal && <input type="hidden" name="no_redirect" value="1" />}

      {/* ① 基础信息 */}
      <Card>
        <CardHeader>
          <CardTitle>基础信息</CardTitle>
          <CardDescription>任务归属广告主与状态</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4 sm:grid-cols-2">
          <div className="space-y-1.5">
            <Label htmlFor="name">任务名称 *</Label>
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
              onChange={(e) => setAdvertiserId(e.target.value)}
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
            <Label htmlFor="product_id">所属产品</Label>
            <select
              id="product_id"
              name="product_id"
              className={selectCls}
              defaultValue={initial?.product_id ?? ""}
            >
              <option value="">（无 / 直挂广告主）</option>
              {(products ?? [])
                .filter((p) => p.advertiser_id === advertiserId)
                .map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name}
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
              <option value="active">投放中</option>
              <option value="paused">已暂停</option>
            </select>
          </div>
          <div className="space-y-1.5 sm:col-span-2">
            <Label htmlFor="landing_url">落地页地址</Label>
            <Input
              id="landing_url"
              name="landing_url"
              type="url"
              placeholder="https://example.com/landing"
              defaultValue={initial?.landing_url ?? ""}
            />
            <p className="text-xs text-muted-foreground">
              用户点击广告后，服务端据此生成带 click_id 的跳转地址
            </p>
          </div>
        </CardContent>
      </Card>

      {/* ② 出价与 KPI */}
      <Card>
        <CardHeader>
          <CardTitle>出价与 KPI</CardTitle>
          <CardDescription>
            设置扣费方式、出价区间与曝光系数
          </CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4 sm:grid-cols-2">
          <div className="space-y-1.5">
            <Label htmlFor="billing_mode">扣费方式 *</Label>
            <select
              id="billing_mode"
              name="billing_mode"
              className={selectCls}
              value={billingMode}
              onChange={(e) => setBillingMode(e.target.value as BillingMode)}
            >
              {billingModeOptions.map((o) => (
                <option key={o.value} value={o.value}>
                  {o.label}
                </option>
              ))}
            </select>
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="target_kpi_type">目标 KPI 类型</Label>
            <select
              id="target_kpi_type"
              name="target_kpi_type"
              className={selectCls}
              defaultValue={initial?.target_kpi_type ?? billingMode}
            >
              {billingModeOptions.map((o) => (
                <option key={o.value} value={o.value}>
                  {o.label}
                </option>
              ))}
            </select>
            <p className="text-xs text-muted-foreground">一般与扣费方式一致</p>
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="target_kpi_value">目标 KPI 值</Label>
            <Input
              id="target_kpi_value"
              name="target_kpi_value"
              type="number"
              step="0.01"
              min="0"
              defaultValue={initial?.target_kpi_value || ""}
            />
          </div>
          <div className="space-y-1.5 sm:col-span-2">
            <Label>出价区间（单价，美元）</Label>
            <div className="flex items-center gap-2">
              <Input
                id="bidding_price_min"
                name="bidding_price_min"
                type="number"
                step="0.01"
                min="0"
                placeholder="下限"
                className="h-8 w-28"
                defaultValue={initial?.bidding_price_min || ""}
              />
              <span className="text-muted-foreground">~</span>
              <Input
                id="bidding_price"
                name="bidding_price"
                type="number"
                step="0.01"
                min="0"
                placeholder="上限"
                className="h-8 w-28"
                defaultValue={initial?.bidding_price || ""}
              />
            </div>
            <p className="text-xs text-muted-foreground">
              下限留空 = 按固定单价（上限）扣费；填了则在 [下限, 上限] 间随机扣费。
              CPM=每千次曝光、CPC=每次点击、CPA=每事件。
            </p>
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="daily_budget">任务日预算（0=跟随广告主）</Label>
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
            <Label htmlFor="consume_speed">曝光系数</Label>
            <Input
              id="consume_speed"
              name="consume_speed"
              type="number"
              step="1"
              min="1"
              max="10"
              defaultValue={initial?.consume_speed ?? 5}
            />
            <p className="text-xs text-muted-foreground">1-10，默认 5，影响下发节奏</p>
          </div>
          {/* 保量份额（guaranteed_min_share / guaranteed_enabled）暂未接入引擎，界面隐藏。
              保留隐藏输入以便编辑其它字段时不清空已存的保量配置。 */}
          <div className="hidden">
            <Label htmlFor="guaranteed_min_share">保量份额（%）</Label>
            <Input
              id="guaranteed_min_share"
              name="guaranteed_min_share"
              type="number"
              step="1"
              min="0"
              max="100"
              defaultValue={
                initial?.guaranteed_min_share
                  ? (initial.guaranteed_min_share * 100).toFixed(0)
                  : ""
              }
            />
            <label className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                name="guaranteed_enabled"
                defaultChecked={initial?.guaranteed_enabled ?? false}
                className="size-4"
              />
              启用保量（最小填充份额）
            </label>
          </div>
        </CardContent>
      </Card>

      {/* ③ 频控 */}
      <Card>
        <CardHeader>
          <CardTitle>频控</CardTitle>
          <CardDescription>任务级频控：限制同一用户下发同一任务的次数</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4 sm:grid-cols-2">
          <div className="space-y-1.5">
            <Label htmlFor="freq_interval_minutes">时间窗（分钟）</Label>
            <Input
              id="freq_interval_minutes"
              name="freq_interval_minutes"
              type="number"
              min="0"
              defaultValue={initial?.freq_interval_minutes || 20}
            />
            <p className="text-xs text-muted-foreground">
              控制 1 的时间窗长度（1~1440，滑动窗口）。0 表示不启用该窗口。
            </p>
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="freq_fatigue_window">窗口内上限</Label>
            <Input
              id="freq_fatigue_window"
              name="freq_fatigue_window"
              type="number"
              min="0"
              defaultValue={initial?.freq_fatigue_window || 3}
            />
            <p className="text-xs text-muted-foreground">
              控制 1：该时间窗内同一用户最多下发同一任务的次数。
            </p>
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="freq_daily_limit">每日上限</Label>
            <Input
              id="freq_daily_limit"
              name="freq_daily_limit"
              type="number"
              min="0"
              defaultValue={initial?.freq_daily_limit || 8}
            />
            <p className="text-xs text-muted-foreground">
              控制 2：每日（滚动 24h）同一用户最多下发同一任务的次数。0 表示不限。
            </p>
          </div>
        </CardContent>
      </Card>

      {/* ④ 关联素材（公共创意库） */}
      <Card>
        <CardHeader>
          <CardTitle>关联素材</CardTitle>
          <CardDescription>
            从公共素材库勾选本任务使用的素材（不勾 = 库内全部可用）
          </CardDescription>
        </CardHeader>
        <CardContent>
          {creatives.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              素材库为空，请先到「素材管理」上传素材
            </p>
          ) : (
            <div className="flex flex-wrap gap-4">
              {creatives.map((c) => (
                <label
                  key={c.id}
                  className="flex items-center gap-2 text-sm"
                >
                  <input
                    type="checkbox"
                    name="creative_ids"
                    value={c.id}
                    defaultChecked={initial?.creative_ids?.includes(c.id)}
                    className="size-4"
                  />
                  {c.name}
                </label>
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      {/* ⑤ 排期 */}
      <Card>
        <CardHeader>
          <CardTitle>投放排期</CardTitle>
        </CardHeader>
        <CardContent className="grid gap-4 sm:grid-cols-2">
          <div className="space-y-1.5">
            <Label htmlFor="start_at">开始时间</Label>
            <Input
              id="start_at"
              name="start_at"
              type="datetime-local"
              defaultValue={initial?.start_at?.slice(0, 16) ?? ""}
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="end_at">结束时间</Label>
            <Input
              id="end_at"
              name="end_at"
              type="datetime-local"
              defaultValue={initial?.end_at?.slice(0, 16) ?? ""}
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="deliver_ttl_minutes">下发有效期（分钟，0=兜底 10）</Label>
            <Input
              id="deliver_ttl_minutes"
              name="deliver_ttl_minutes"
              type="number"
              step="1"
              min="0"
              placeholder="留空=10"
              defaultValue={initial?.deliver_ttl_minutes || ""}
            />
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
        {pending ? "保存中…" : isEdit ? "保存修改" : "创建任务"}
      </Button>
    </form>
  );
}
