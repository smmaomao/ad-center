"use client";

import { useState, useTransition } from "react";
import { useRouter } from "next/navigation";
import { Button } from "@/components/ui/button";
import { PageHeader } from "@/components/page-header";
import { CREATIVE_STYLES } from "@/lib/go-api";
import { deleteCreativeAction } from "./actions";
import { CreativeFormModal } from "./creative-form-modal";
import { CreativeViewModal } from "./creative-view-modal";
import type { AdminAdvertiser, AdminApp, AdminCreative } from "@/lib/go-api";

const STATUS_LABEL: Record<string, string> = {
  active: "上架",
  testing: "测试中",
  paused: "下架",
};

function formatBytes(n: number): string {
  if (!n || n < 0) return "—";
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
}
function formatDuration(ms: number): string {
  if (!ms || ms <= 0) return "—";
  const s = ms / 1000;
  if (s < 60) return `${s.toFixed(1)}s`;
  const m = Math.floor(s / 60);
  const rem = Math.round(s % 60);
  return `${m}m${rem}s`;
}

function StatusBadge({ status }: { status: AdminCreative["status"] }) {
  const map: Record<AdminCreative["status"], string> = {
    active: "bg-emerald-50 text-emerald-700",
    testing: "bg-amber-50 text-amber-700",
    paused: "bg-zinc-100 text-zinc-600",
  };
  return (
    <span
      className={`inline-flex rounded-full px-2 py-0.5 text-xs font-medium ${map[status]}`}
    >
      {STATUS_LABEL[status] ?? status}
    </span>
  );
}

export function CreativesClient({
  creatives,
  advertisers,
  apps,
  error,
  canWrite,
}: {
  creatives: AdminCreative[];
  advertisers: AdminAdvertiser[];
  apps: AdminApp[];
  error: string | null;
  canWrite: boolean;
}) {
  const router = useRouter();
  const [createOpen, setCreateOpen] = useState(false);
  const [editing, setEditing] = useState<AdminCreative | null>(null);
  const [viewing, setViewing] = useState<AdminCreative | null>(null);
  const [isPending, startTransition] = useTransition();
  const [deletingId, setDeletingId] = useState<string | null>(null);

  const advName = (id: string) =>
    id ? advertisers.find((a) => a.id === id)?.name ?? "—" : "公共库";
  const appName = (id: string) => apps.find((a) => a.id === id)?.name ?? id;
  const styleLabel = (k: string) =>
    CREATIVE_STYLES.find((s) => s.key === k)?.label ?? k;

  // 列表素材单元格里展示自动解析出的 大小/尺寸/时长（html 类型无这些字段）
  const mediaMeta = (c: AdminCreative) => {
    const parts: string[] = [];
    if (c.file_size_bytes > 0) parts.push(formatBytes(c.file_size_bytes));
    if (c.width > 0) parts.push(`${c.width}×${c.height}`);
    if (c.media_type === "video" && c.duration_ms > 0)
      parts.push(formatDuration(c.duration_ms));
    return parts.length ? ` · ${parts.join(" · ")}` : "";
  };

  const handleDelete = (c: AdminCreative) => {
    if (!window.confirm(`确定删除素材「${c.name}」？`)) return;
    setDeletingId(c.id);
    startTransition(async () => {
      const fd = new FormData();
      fd.set("id", c.id);
      await deleteCreativeAction(fd);
      setDeletingId(null);
      router.refresh();
    });
  };

  return (
    <div className="space-y-6">
      <PageHeader
        title="素材管理"
        description={`公共素材库 · 共 ${creatives.length} 个素材`}
        actions={
          canWrite ? (
            <Button onClick={() => setCreateOpen(true)}>新建素材</Button>
          ) : undefined
        }
      />

      {error && (
        <div className="surface-card border border-destructive/40 p-4 text-sm text-destructive">
          加载失败：{error}
        </div>
      )}

      {creatives.length > 0 && (
        <div className="surface-card overflow-hidden">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border bg-muted/40 text-left text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                <th className="px-5 py-3">ID</th>
                <th className="px-5 py-3">素材</th>
                <th className="px-5 py-3">展现样式</th>
                <th className="px-5 py-3">投放 App</th>
                <th className="px-5 py-3">状态</th>
                <th className="px-5 py-3 text-right">操作</th>
              </tr>
            </thead>
            <tbody>
              {creatives.map((c) => (
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
                    <div className="text-xs text-muted-foreground">
                      {advName(c.advertiser_id)}
                      {mediaMeta(c)}
                    </div>
                  </td>
                  <td className="px-5 py-3">
                    <div className="flex flex-wrap gap-1">
                      {(c.styles ?? []).length === 0 ? (
                        <span className="text-xs text-muted-foreground">
                          未设置
                        </span>
                      ) : (
                        (c.styles ?? []).map((s) => (
                          <span
                            key={s}
                            className="rounded bg-muted px-1.5 py-0.5 text-xs"
                          >
                            {styleLabel(s)}
                          </span>
                        ))
                      )}
                    </div>
                  </td>
                  <td className="px-5 py-3 text-xs">
                    {(c.target_apps ?? []).length === 0
                      ? "全部 App"
                      : (c.target_apps ?? []).map(appName).join("、")}
                  </td>
                  <td className="px-5 py-3">
                    <StatusBadge status={c.status} />
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

      {creatives.length === 0 && !error && (
        <div className="surface-card p-10 text-center text-sm text-muted-foreground">
          还没有素材，点击右上角「新建素材」创建
        </div>
      )}

      {createOpen && (
        <CreativeFormModal
          advertisers={advertisers}
          apps={apps}
          canWrite={canWrite}
          onClose={() => setCreateOpen(false)}
        />
      )}
      {editing && (
        <CreativeFormModal
          advertisers={advertisers}
          apps={apps}
          initial={editing}
          canWrite={canWrite}
          onClose={() => setEditing(null)}
        />
      )}
      {viewing && (
        <CreativeViewModal
          creative={viewing}
          advertiserName={advName(viewing.advertiser_id)}
          appName={appName}
          onClose={() => setViewing(null)}
        />
      )}
    </div>
  );
}
