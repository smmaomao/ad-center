"use client";

import { useState, useTransition } from "react";
import { useRouter } from "next/navigation";
import { Button } from "@/components/ui/button";
import { PageHeader } from "@/components/page-header";
import { deleteCampaignAction } from "./actions";
import { CampaignFormModal } from "./campaign-form-modal";
import { CampaignViewModal } from "./campaign-view-modal";
import type {
  AdminAdvertiser,
  AdminCreative,
  AdminCampaign,
  AdminProduct,
} from "@/lib/go-api";

function StatusBadge({ status }: { status: AdminCampaign["status"] }) {
  const map: Record<AdminCampaign["status"], string> = {
    active: "bg-emerald-50 text-emerald-700",
    paused: "bg-zinc-100 text-zinc-600",
  };
  const label: Record<AdminCampaign["status"], string> = {
    active: "投放中",
    paused: "已暂停",
  };
  return (
    <span
      className={`inline-flex rounded-full px-2 py-0.5 text-xs font-medium ${map[status]}`}
    >
      {label[status]}
    </span>
  );
}

export function CampaignsClient({
  campaigns,
  advertisers,
  creatives,
  products,
  error,
  canWrite,
}: {
  campaigns: AdminCampaign[];
  advertisers: AdminAdvertiser[];
  creatives: AdminCreative[];
  products: AdminProduct[];
  error: string | null;
  canWrite: boolean;
}) {
  const router = useRouter();
  const [createOpen, setCreateOpen] = useState(false);
  const [editing, setEditing] = useState<AdminCampaign | null>(null);
  const [viewing, setViewing] = useState<AdminCampaign | null>(null);
  const [isPending, startTransition] = useTransition();
  const [deletingId, setDeletingId] = useState<string | null>(null);

  const handleDelete = (c: AdminCampaign) => {
    if (!window.confirm(`确定删除广告任务「${c.name}」？`)) return;
    setDeletingId(c.id);
    startTransition(async () => {
      const fd = new FormData();
      fd.set("id", c.id);
      await deleteCampaignAction(fd);
      setDeletingId(null);
      router.refresh();
    });
  };

  return (
    <div className="space-y-6">
      <PageHeader
        title="广告任务管理"
        description={`共 ${campaigns.length} 个广告任务，挂于广告主之下`}
        actions={
          canWrite ? (
            <Button onClick={() => setCreateOpen(true)}>新建任务</Button>
          ) : undefined
        }
      />

      {error && (
        <div className="surface-card border border-destructive/40 p-4 text-sm text-destructive">
          加载失败：{error}
        </div>
      )}

      {campaigns.length > 0 && (
        <div className="surface-card overflow-hidden">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border bg-muted/40 text-left text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                <th className="px-5 py-3">ID</th>
                <th className="px-5 py-3">任务</th>
                <th className="px-5 py-3">广告主</th>
                <th className="px-5 py-3">产品</th>
                <th className="px-5 py-3">状态</th>
                <th className="px-5 py-3">出价 / KPI</th>
                <th className="px-5 py-3">计费 / 频控</th>
                <th className="px-5 py-3 text-right">关联素材</th>
                <th className="px-5 py-3 text-right">操作</th>
              </tr>
            </thead>
            <tbody>
              {campaigns.map((c) => (
                <tr
                  key={c.id}
                  className="border-b border-border/70 last:border-0 transition-colors hover:bg-muted/50"
                >
                  <td className="px-5 py-3 font-mono text-[11px] text-muted-foreground" title={c.id}>
                    {c.id}
                  </td>
                  <td className="px-5 py-3">
                    <button
                      type="button"
                      onClick={() => setViewing(c)}
                      className="text-left font-semibold text-foreground transition-colors hover:text-brand hover:underline"
                    >
                      {c.name}
                    </button>
                  </td>
                  <td className="px-5 py-3 text-xs">{c.advertiser_name}</td>
                  <td className="px-5 py-3 text-xs">{c.product_name ?? "—"}</td>
                  <td className="px-5 py-3">
                    <StatusBadge status={c.status} />
                  </td>
                  <td className="px-5 py-3 tabular-nums">
                    <div>
                      {c.bidding_mode.toUpperCase()} ${c.bidding_price}
                    </div>
                    <div className="text-xs text-muted-foreground">
                      KPI ${c.target_cpi}
                    </div>
                  </td>
                  <td className="px-5 py-3 text-xs">
                    <div>{c.billing_mode.toUpperCase()}</div>
                    <div className="text-muted-foreground">
                      {c.freq_daily_limit}/天 · {c.freq_interval_minutes}min
                    </div>
                  </td>
                  <td className="px-5 py-3 text-right tabular-nums">
                    {c.creative_ids?.length ?? 0}
                  </td>
                  <td className="px-5 py-3">
                    <div className="flex justify-end gap-3 text-xs">
                      <button
                        type="button"
                        className="text-muted-foreground hover:underline"
                        onClick={() => setViewing(c)}
                      >
                        查看
                      </button>
                      {canWrite && (
                        <button
                          type="button"
                          className="font-medium text-brand hover:underline"
                          onClick={() => setEditing(c)}
                        >
                          编辑
                        </button>
                      )}
                      {canWrite && (
                        <button
                          type="button"
                          disabled={isPending && deletingId === c.id}
                          className="text-destructive hover:underline disabled:opacity-50"
                          onClick={() => handleDelete(c)}
                        >
                          {isPending && deletingId === c.id ? "删除中…" : "删除"}
                        </button>
                      )}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {campaigns.length === 0 && !error && (
        <div className="surface-card p-10 text-center text-sm text-muted-foreground">
          还没有广告任务，点击右上角「新建任务」
        </div>
      )}

      {createOpen && (
        <CampaignFormModal
          advertisers={advertisers}
          creatives={creatives}
          products={products}
          canWrite={canWrite}
          onClose={() => setCreateOpen(false)}
        />
      )}
      {editing && (
        <CampaignFormModal
          advertisers={advertisers}
          creatives={creatives}
          products={products}
          initial={editing}
          canWrite={canWrite}
          onClose={() => setEditing(null)}
        />
      )}
      {viewing && (
        <CampaignViewModal campaign={viewing} onClose={() => setViewing(null)} />
      )}
    </div>
  );
}
