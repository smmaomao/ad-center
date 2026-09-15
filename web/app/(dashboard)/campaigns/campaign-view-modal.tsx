"use client";

import type { ReactNode } from "react";
import { Modal } from "@/components/modal";
import { Button } from "@/components/ui/button";
import type { AdminCampaign } from "@/lib/go-api";

const STATUS_LABEL: Record<string, string> = {
  active: "投放中",
  paused: "已暂停",
};

function Field({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="space-y-1">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="text-sm font-medium text-foreground">{value}</dd>
    </div>
  );
}

export function CampaignViewModal({
  campaign,
  onClose,
}: {
  campaign: AdminCampaign;
  onClose: () => void;
}) {
  const status = campaign.status;
  return (
    <Modal
      open
      size="lg"
      title={campaign.name}
      description={`任务 ID：${campaign.id}`}
      onClose={onClose}
      footer={<Button variant="outline" onClick={onClose}>关闭</Button>}
    >
      <div className="space-y-6">
        <div className="flex items-center gap-2">
          <span
            className={`inline-flex rounded-full px-2 py-0.5 text-xs font-medium ${
              status === "active"
                ? "bg-emerald-50 text-emerald-700"
                : "bg-zinc-100 text-zinc-600"
            }`}
          >
            {STATUS_LABEL[status] ?? status}
          </span>
          <span className="text-sm text-muted-foreground">
            所属广告主：{campaign.advertiser_name}
            {campaign.product_name ? ` · 产品：${campaign.product_name}` : ""}
          </span>
        </div>

        <section>
          <h3 className="mb-3 text-xs font-semibold uppercase tracking-wider text-muted-foreground">
            出价与 KPI
          </h3>
          <dl className="grid gap-x-6 gap-y-4 sm:grid-cols-2">
            <Field label="出价口径" value={campaign.bidding_mode.toUpperCase()} />
            <Field label="目标 KPI（target_cpi）" value={`$${campaign.target_cpi}`} />
            <Field label="出价（单价）" value={`$${campaign.bidding_price}`} />
            <Field
              label="出价下限"
              value={campaign.bidding_price_min ? `$${campaign.bidding_price_min}` : "—"}
            />
            <Field
              label="任务日预算"
              value={campaign.daily_budget ? `$${campaign.daily_budget}` : "跟随广告主"}
            />
            <Field
              label="实际 CPI（事件回写）"
              value={campaign.actual_cpi ? `$${campaign.actual_cpi.toFixed(2)}` : "—"}
            />
            <Field
              label="今日消耗"
              value={campaign.spent_today ? `$${campaign.spent_today.toFixed(2)}` : "$0.00"}
            />
            <Field
              label="消耗节奏"
              value={
                (
                  {
                    even: "均匀消耗",
                    accelerated: "加速消耗",
                    asap: "尽快花完",
                  } as Record<string, string>
                )[campaign.consume_speed] ?? campaign.consume_speed ?? "—"
              }
            />
            <Field
              label="保量份额"
              value={
                campaign.guaranteed_enabled
                  ? `${((campaign.guaranteed_min_share ?? 0) * 100).toFixed(0)}%`
                  : "未启用"
              }
            />
          </dl>
        </section>

        <section>
          <h3 className="mb-3 text-xs font-semibold uppercase tracking-wider text-muted-foreground">
            计费与频控
          </h3>
          <dl className="grid gap-x-6 gap-y-4 sm:grid-cols-2">
            <Field label="扣费方式" value={campaign.billing_mode.toUpperCase()} />
            <Field label="日频控（次/天）" value={campaign.freq_daily_limit} />
            <Field label="最小间隔（分钟）" value={campaign.freq_interval_minutes} />
            <Field label="疲劳窗口（天）" value={campaign.freq_fatigue_window} />
          </dl>
        </section>

        <section>
          <h3 className="mb-3 text-xs font-semibold uppercase tracking-wider text-muted-foreground">
            投放与素材
          </h3>
          <dl className="grid gap-x-6 gap-y-4 sm:grid-cols-2">
            <Field
              label="关联素材数"
              value={`${campaign.creative_ids?.length ?? 0} 个（可服务多个任务）`}
            />
            <Field
              label="排期"
              value={
                campaign.start_at || campaign.end_at
                  ? `${campaign.start_at?.slice(0, 16) ?? "—"} ~ ${campaign.end_at?.slice(0, 16) ?? "—"}`
                  : "长期"
              }
            />
          </dl>
        </section>
      </div>
    </Modal>
  );
}
