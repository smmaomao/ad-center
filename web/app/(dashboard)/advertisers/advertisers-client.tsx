"use client";

import { useState, useTransition } from "react";
import { useRouter } from "next/navigation";
import { Button } from "@/components/ui/button";
import { Modal } from "@/components/modal";
import { PageHeader } from "@/components/page-header";
import { deleteAdvertiserAction } from "./actions";
import { AdvertiserFormModal } from "./advertiser-form-modal";
import { AdvertiserViewModal } from "./advertiser-view-modal";
import type { AdminAdvertiser } from "@/lib/go-api";

export function AdvertisersClient({
  advertisers,
  error,
  canWrite,
}: {
  advertisers: AdminAdvertiser[];
  error: string | null;
  canWrite: boolean;
}) {
  const router = useRouter();
  const [createOpen, setCreateOpen] = useState(false);
  const [editing, setEditing] = useState<AdminAdvertiser | null>(null);
  const [viewing, setViewing] = useState<AdminAdvertiser | null>(null);
  const [isPending, startTransition] = useTransition();
  const [deletingId, setDeletingId] = useState<string | null>(null);

  const handleDelete = (a: AdminAdvertiser) => {
    if (!window.confirm(`确定删除广告主「${a.name}」？删除后不再参与投放（软删，历史数据保留）。`)) {
      return;
    }
    setDeletingId(a.id);
    startTransition(async () => {
      await deleteAdvertiserAction(a.id);
      setDeletingId(null);
      router.refresh();
    });
  };

  return (
    <div className="space-y-6">
      <PageHeader
        title="广告主管理"
        description={`共 ${advertisers.length} 个广告主`}
        actions={
          canWrite ? (
            <Button onClick={() => setCreateOpen(true)}>新增广告主</Button>
          ) : undefined
        }
      />

      {error && (
        <div className="surface-card border border-destructive/40 p-4 text-sm text-destructive">
          加载失败：{error}
        </div>
      )}

      {advertisers.length > 0 && (
        <div className="surface-card overflow-hidden">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border bg-muted/40 text-left text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                <th className="px-5 py-3">ID</th>
                <th className="px-5 py-3">广告主</th>
                <th className="px-5 py-3">联系方式</th>
                <th className="px-5 py-3">备注</th>
                <th className="px-5 py-3">创建时间</th>
                <th className="px-5 py-3 text-right">操作</th>
              </tr>
            </thead>
            <tbody>
              {advertisers.map((a) => {
                return (
                  <tr
                    key={a.id}
                    className="border-b border-border/70 last:border-0 transition-colors hover:bg-muted/50"
                  >
                    <td className="px-5 py-3 font-mono text-[11px] text-muted-foreground" title={a.id}>
                      {a.id}
                    </td>
                    <td className="px-5 py-3">
                      <button
                        type="button"
                        onClick={() => setViewing(a)}
                        className="text-left font-semibold text-foreground transition-colors hover:text-brand hover:underline"
                      >
                        {a.name}
                      </button>
                    </td>
                    <td className="px-5 py-3 text-muted-foreground">
                      {a.contact || "—"}
                    </td>
                    <td className="px-5 py-3 text-muted-foreground">
                      {a.notes || "—"}
                    </td>
                    <td className="px-5 py-3 tabular-nums text-muted-foreground">
                      {a.created_at ?? "—"}
                    </td>
                    <td className="px-5 py-3">
                      <div className="flex justify-end gap-3 text-xs">
                        <button
                          type="button"
                          className="text-muted-foreground hover:underline"
                          onClick={() => setViewing(a)}
                        >
                          查看
                        </button>
                        {canWrite && (
                          <button
                            type="button"
                            className="font-medium text-brand hover:underline"
                            onClick={() => setEditing(a)}
                          >
                            编辑
                          </button>
                        )}
                        {canWrite && (
                          <button
                            type="button"
                            disabled={isPending && deletingId === a.id}
                            className="text-destructive hover:underline disabled:opacity-50"
                            onClick={() => handleDelete(a)}
                          >
                            {isPending && deletingId === a.id ? "删除中…" : "删除"}
                          </button>
                        )}
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}

      {createOpen && (
        <AdvertiserFormModal
          canWrite={canWrite}
          onClose={() => setCreateOpen(false)}
        />
      )}
      {editing && (
        <AdvertiserFormModal
          initial={editing}
          canWrite={canWrite}
          onClose={() => setEditing(null)}
        />
      )}
      {viewing && (
        <AdvertiserViewModal advertiser={viewing} onClose={() => setViewing(null)} />
      )}
    </div>
  );
}
