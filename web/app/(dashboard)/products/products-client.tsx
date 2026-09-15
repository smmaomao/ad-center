"use client";

import { useState, useTransition } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { Button } from "@/components/ui/button";
import { PageHeader } from "@/components/page-header";
import { deleteProductAction } from "./actions";
import { ProductFormModal } from "./product-form-modal";
import { ProductViewModal } from "./product-view-modal";
import type { AdminAdvertiser, AdminProduct } from "@/lib/go-api";

function StatusBadge({ status }: { status: AdminProduct["status"] }) {
  const map: Record<AdminProduct["status"], string> = {
    active: "bg-emerald-50 text-emerald-700",
    paused: "bg-zinc-100 text-zinc-600",
  };
  const label: Record<AdminProduct["status"], string> = {
    active: "启用",
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

export function ProductsClient({
  products,
  advertisers,
  rollups,
  error,
  canWrite,
  filterAdvertiser,
}: {
  products: AdminProduct[];
  advertisers: AdminAdvertiser[];
  rollups: Record<
    string,
    { count: number; budget: number; spent: number; guaranteed: number }
  >;
  error: string | null;
  canWrite: boolean;
  filterAdvertiser?: string | null;
}) {
  const router = useRouter();
  const [createOpen, setCreateOpen] = useState(false);
  const [editing, setEditing] = useState<AdminProduct | null>(null);
  const [viewing, setViewing] = useState<AdminProduct | null>(null);
  const [isPending, startTransition] = useTransition();
  const [deletingId, setDeletingId] = useState<string | null>(null);

  const advName = (id: string) =>
    advertisers.find((a) => a.id === id)?.name ?? "—";

  const handleDelete = (p: AdminProduct) => {
    if (
      !window.confirm(
        `确定删除产品「${p.name}」？其下广告任务将变为直挂广告主（不会被删除）。`,
      )
    )
      return;
    setDeletingId(p.id);
    startTransition(async () => {
      const fd = new FormData();
      fd.set("id", p.id);
      await deleteProductAction(fd);
      setDeletingId(null);
      router.refresh();
    });
  };

  return (
    <div className="space-y-6">
      <PageHeader
        title="产品管理"
        description={`共 ${products.length} 个产品，挂在广告主之下（一个广告主可有多款产品）`}
        actions={
          canWrite ? (
            <Button onClick={() => setCreateOpen(true)}>新增产品</Button>
          ) : undefined
        }
      />

      {error && (
        <div className="surface-card border border-destructive/40 p-4 text-sm text-destructive">
          加载失败：{error}
        </div>
      )}

      {filterAdvertiser && (
        <div className="flex items-center gap-2 text-sm">
          <span className="text-muted-foreground">已按广告主筛选：</span>
          <span className="rounded-full bg-muted px-2 py-0.5 font-medium">
            {filterAdvertiser}
          </span>
          <Link href="/products" className="text-brand hover:underline">
            清除筛选
          </Link>
        </div>
      )}

      {products.length > 0 && (
        <div className="surface-card overflow-hidden">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border bg-muted/40 text-left text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                <th className="px-5 py-3">ID</th>
                <th className="px-5 py-3">产品</th>
                <th className="px-5 py-3">所属广告主</th>
                <th className="px-5 py-3">状态</th>
                <th className="px-5 py-3">任务汇总</th>
                <th className="px-5 py-3 text-right">日预算</th>
                <th className="px-5 py-3 text-right">操作</th>
              </tr>
            </thead>
            <tbody>
              {products.map((p) => (
                <tr
                  key={p.id}
                  className="border-b border-border/70 last:border-0 transition-colors hover:bg-muted/50"
                >
                  <td className="px-5 py-3 font-mono text-[11px] text-muted-foreground" title={p.id}>
                    {p.id}
                  </td>
                  <td className="px-5 py-3">
                    <button
                      type="button"
                      onClick={() => setViewing(p)}
                      className="text-left font-semibold text-foreground transition-colors hover:text-brand hover:underline"
                    >
                      {p.name}
                    </button>
                    {p.notes && (
                      <div className="text-xs text-muted-foreground">
                        {p.notes}
                      </div>
                    )}
                  </td>
                  <td className="px-5 py-3 text-xs">{advName(p.advertiser_id)}</td>
                  <td className="px-5 py-3">
                    <StatusBadge status={p.status} />
                  </td>
                  <td className="px-5 py-3 text-xs tabular-nums text-muted-foreground">
                    {(() => {
                      const r = rollups[p.id];
                      if (!r || r.count === 0) return "—";
                      return `${r.count} 任务 · 预算 $${r.budget.toFixed(0)} · 消耗 $${r.spent.toFixed(0)}`;
                    })()}
                  </td>
                  <td className="px-5 py-3 text-right tabular-nums">
                    {p.daily_budget ? `$${p.daily_budget}` : "—"}
                  </td>
                  <td className="px-5 py-3">
                    <div className="flex justify-end gap-3 text-xs">
                      <button
                        type="button"
                        className="text-muted-foreground hover:underline"
                        onClick={() => setViewing(p)}
                      >
                        查看
                      </button>
                      {canWrite && (
                        <button
                          type="button"
                          className="font-medium text-brand hover:underline"
                          onClick={() => setEditing(p)}
                        >
                          编辑
                        </button>
                      )}
                      {canWrite && (
                        <button
                          type="button"
                          disabled={isPending && deletingId === p.id}
                          className="text-destructive hover:underline disabled:opacity-50"
                          onClick={() => handleDelete(p)}
                        >
                          {isPending && deletingId === p.id
                            ? "删除中…"
                            : "删除"}
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

      {products.length === 0 && !error && (
        <div className="surface-card p-10 text-center text-sm text-muted-foreground">
          还没有产品，点击右上角「新增产品」
        </div>
      )}

      {createOpen && (
        <ProductFormModal
          advertisers={advertisers}
          canWrite={canWrite}
          onClose={() => setCreateOpen(false)}
        />
      )}
      {editing && (
        <ProductFormModal
          advertisers={advertisers}
          initial={editing}
          canWrite={canWrite}
          onClose={() => setEditing(null)}
        />
      )}
      {viewing && (
        <ProductViewModal
          product={viewing}
          advertiserName={advName(viewing.advertiser_id)}
          onClose={() => setViewing(null)}
        />
      )}
    </div>
  );
}
