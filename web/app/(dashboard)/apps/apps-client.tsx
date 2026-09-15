"use client";

import { useState, useTransition } from "react";
import { useRouter } from "next/navigation";
import { Button } from "@/components/ui/button";
import { Modal } from "@/components/modal";
import { PageHeader } from "@/components/page-header";
import { MaskedKey } from "@/components/masked-key";
import type { AdminApp } from "@/lib/go-api";
import { CreateAppModal } from "./create-app-modal";
import { EditAppModal } from "./edit-app-modal";
import { deleteAppAction } from "./actions";

function StatusBadge({ status }: { status: string }) {
  const active = status === "active";
  return (
    <span
      className={
        "inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium ring-1 " +
        (active
          ? "bg-emerald-50 text-emerald-700 ring-emerald-600/20"
          : "bg-muted text-foreground/60 ring-border")
      }
    >
      <span
        className={
          "h-1.5 w-1.5 rounded-full " +
          (active ? "bg-emerald-500" : "bg-foreground/30")
        }
      />
      {active ? "启用" : "暂停"}
    </span>
  );
}

export function AppsClient({
  apps,
  canWrite,
}: {
  apps: AdminApp[];
  canWrite: boolean;
}) {
  const router = useRouter();
  const [createOpen, setCreateOpen] = useState(false);
  const [editing, setEditing] = useState<AdminApp | null>(null);
  const [isPending, startTransition] = useTransition();
  const [deletingId, setDeletingId] = useState<string | null>(null);

  const handleDelete = (a: AdminApp) => {
    if (!window.confirm(`确定删除 App「${a.name}」？删除后该 App 的签名密钥与 API Key 立即失效，历史归因数据保留（软删）。`)) return;
    setDeletingId(a.id);
    startTransition(async () => {
      await deleteAppAction(a.id);
      setDeletingId(null);
      router.refresh();
    });
  };

  return (
    <div className="space-y-6">
      <PageHeader
        title="应用管理"
        description="注册对接广告系统的 App，配置业务后端回调与 S2S 签名密钥"
        actions={
          canWrite ? (
            <Button onClick={() => setCreateOpen(true)}>注册 App</Button>
          ) : undefined
        }
      />

      <div className="surface-card rise-1 overflow-hidden">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-border bg-muted/40 text-left text-xs font-semibold uppercase tracking-wider text-muted-foreground">
              <th className="px-5 py-3">应用名称</th>
              <th className="px-5 py-3">Ad_App_Id</th>
              <th className="px-5 py-3">API Key</th>
              <th className="px-5 py-3">业务回调地址</th>
              <th className="px-5 py-3">状态</th>
              <th className="px-5 py-3">创建时间</th>
              <th className="px-5 py-3 text-right">操作</th>
            </tr>
          </thead>
          <tbody>
            {apps.length === 0 ? (
              <tr>
                <td colSpan={7} className="px-5 py-16 text-center">
                  <div className="mx-auto flex max-w-xs flex-col items-center gap-2 text-sm text-muted-foreground">
                    <div className="grid h-11 w-11 place-items-center rounded-full bg-muted text-foreground/40">
                      <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
                        <rect x="3" y="4" width="18" height="16" rx="2" />
                        <path d="M3 9h18M8 14h8" />
                      </svg>
                    </div>
                    还没有 App，点击右上角「注册 App」创建
                  </div>
                </td>
              </tr>
            ) : (
              apps.map((a) => (
                <tr
                  key={a.id}
                  className="border-b border-border/70 last:border-0 transition-colors hover:bg-muted/50"
                >
                  <td className="px-5 py-3">
                    <button
                      type="button"
                      onClick={() => setEditing(a)}
                      className="text-left font-semibold text-foreground transition-colors hover:text-brand hover:underline"
                    >
                      {a.name}
                    </button>
                  </td>
                  <td className="px-5 py-3 font-mono text-xs text-muted-foreground">
                    {a.id}
                  </td>
                  <td className="px-5 py-3">
                    {a.api_key ? (
                      <MaskedKey value={a.api_key} />
                    ) : (
                      <span className="text-xs text-muted-foreground/50">—</span>
                    )}
                  </td>
                  <td className="px-5 py-3 text-xs text-muted-foreground">
                    {a.callback_url ? (
                      <span className="break-all">{a.callback_url}</span>
                    ) : (
                      "—"
                    )}
                  </td>
                  <td className="px-5 py-3">
                    <StatusBadge status={a.status} />
                  </td>
                  <td className="px-5 py-3 text-xs text-muted-foreground">
                    {a.created_at || "—"}
                  </td>
                  <td className="px-5 py-3 text-right">
                    <div className="flex items-center justify-end gap-3">
                      <button
                        type="button"
                        onClick={() => setEditing(a)}
                        className="text-xs font-semibold text-muted-foreground transition-colors hover:text-brand hover:underline"
                      >
                        {canWrite ? "编辑" : "查看"}
                      </button>
                      {canWrite && (
                        <button
                          type="button"
                          onClick={() => handleDelete(a)}
                          disabled={isPending && deletingId === a.id}
                          className="text-xs font-semibold text-red-600 transition-colors hover:text-red-700 hover:underline disabled:opacity-50"
                        >
                          {isPending && deletingId === a.id ? "删除中…" : "删除"}
                        </button>
                      )}
                    </div>
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>

      {createOpen && (
        <CreateAppModal
          canWrite={canWrite}
          onClose={() => {
            setCreateOpen(false);
            router.refresh();
          }}
        />
      )}
      {editing && (
        <EditAppModal
          app={editing}
          canWrite={canWrite}
          onClose={() => {
            setEditing(null);
            router.refresh();
          }}
        />
      )}
    </div>
  );
}
