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
            <Field
              label="目标 KPI 类型"
              value={campaign.target_kpi_type ? String(campaign.target_kpi_type).toUpperCase() : "—"}
            />
            <Field
              label="目标 KPI 值"
              value={campaign.target_kpi_value ? `$${campaign.target_kpi_value}` : "—"}
            />
            <Field
              label="出价区间（美元）"
              value={
                campaign.bidding_price_min > 0
                  ? `$${campaign.bidding_price_min} ~ $${campaign.bidding_price}`
                  : `$${campaign.bidding_price}（固定单价）`
              }
            />
            <Field
              label="任务日预算"
              value={campaign.daily_budget ? `$${campaign.daily_budget}` : "跟随广告主"}
            />
            <Field
              label="今日消耗"
              value={campaign.spent_today ? `$${campaign.spent_today.toFixed(2)}` : "$0.00"}
            />
            <Field
              label="曝光系数"
              value={campaign.consume_speed ? String(campaign.consume_speed) : "5"}
            />
          </dl>
        </section>

        <section>
          <h3 className="mb-3 text-xs font-semibold uppercase tracking-wider text-muted-foreground">
            计费与频控
          </h3>
          <dl className="grid gap-x-6 gap-y-4 sm:grid-cols-2">
            <Field label="扣费方式" value={campaign.billing_mode.toUpperCase()} />
            <Field label="时间窗（分钟）" value={campaign.freq_interval_minutes} />
            <Field label="窗口内上限" value={campaign.freq_fatigue_window} />
            <Field label="每日上限" value={campaign.freq_daily_limit} />
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
