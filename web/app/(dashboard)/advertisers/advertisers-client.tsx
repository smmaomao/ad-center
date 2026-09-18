"use client";

import { useState, useTransition } from "react";
import { useRouter } from "next/navigation";
import { Button } from "@/components/ui/button";
import { Modal } from "@/components/modal";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { PageHeader } from "@/components/page-header";
import { deleteAdvertiserAction, setWalletGateAction } from "./actions";
import { AdvertiserFormModal } from "./advertiser-form-modal";
import { AdvertiserViewModal } from "./advertiser-view-modal";
import { WalletFlowModal } from "./wallet-flow-modal";
import { RechargeModal } from "./recharge-modal";
import type { AdminAdvertiser } from "@/lib/go-api";

function fmtMoney(n: number): string {
  return "$" + (n ?? 0).toLocaleString("en-US", {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  });
}

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
  const [flowAdv, setFlowAdv] = useState<AdminAdvertiser | null>(null);
  const [rechargeAdv, setRechargeAdv] = useState<AdminAdvertiser | null>(null);
  const [isPending, startTransition] = useTransition();
  const [deletingId, setDeletingId] = useState<string | null>(null);
  const [gateId, setGateId] = useState<string | null>(null);
  const [confirmGate, setConfirmGate] = useState<AdminAdvertiser | null>(null);
  const [gateError, setGateError] = useState<string | null>(null);

  // 实际执行闸门切换；成功后关闭确认弹窗并刷新列表。
  const runToggleGate = (a: AdminAdvertiser, enabling: boolean) => {
    setGateError(null);
    setGateId(a.id);
    startTransition(async () => {
      const res = await setWalletGateAction(a.id, enabling);
      setGateId(null);
      if (res.error) {
        setGateError(res.error);
        return;
      }
      setConfirmGate(null);
      router.refresh();
    });
  };

  // 启用/关闭总钱包闸。启用且余额为 0 时会立即停投，故先弹 UI 二次确认。
  const handleToggleGate = (a: AdminAdvertiser) => {
    const enabling = !a.wallet_enabled;
    if (enabling && a.wallet_balance <= 0) {
      setConfirmGate(a);
      return;
    }
    runToggleGate(a, enabling);
  };

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

      {gateError && !confirmGate && (
        <div className="surface-card border border-destructive/40 p-4 text-sm text-destructive">
          {gateError}
        </div>
      )}

      {advertisers.length > 0 && (
        <div className="surface-card overflow-hidden">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border bg-muted/40 text-left text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                <th className="px-5 py-3">ID</th>
                <th className="px-5 py-3">广告主</th>
                <th className="px-5 py-3">余额</th>
                <th className="px-5 py-3">钱包闸门</th>
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
                    <td className="px-5 py-3">
                      <div className="flex items-center gap-3">
                        <span className="font-medium tabular-nums text-foreground">
                          {fmtMoney(a.wallet_balance)}
                        </span>
                        {canWrite && (
                          <button
                            type="button"
                            className="text-xs font-medium text-brand hover:underline"
                            onClick={() => setRechargeAdv(a)}
                          >
                            充值
                          </button>
                        )}
                      </div>
                    </td>
                    <td className="px-5 py-3">
                      <div className="flex items-center gap-3">
                        <span
                          className={
                            a.wallet_enabled
                              ? "text-emerald-600 dark:text-emerald-400"
                              : "text-muted-foreground"
                          }
                          title={
                            a.wallet_enabled
                              ? "已启用：余额为投放硬顶，余额耗尽会停投其下全部广告"
                              : "未启用：不受总余额限制（存量广告主）"
                          }
                        >
                          {a.wallet_enabled ? "是" : "否"}
                        </span>
                        {canWrite && (
                          <button
                            type="button"
                            disabled={isPending && gateId === a.id}
                            className={
                              a.wallet_enabled
                                ? "text-xs text-destructive hover:underline disabled:opacity-50"
                                : "text-xs font-medium text-brand hover:underline disabled:opacity-50"
                            }
                            onClick={() => handleToggleGate(a)}
                          >
                            {isPending && gateId === a.id
                              ? "处理中…"
                              : a.wallet_enabled
                                ? "关闭"
                                : "启动"}
                          </button>
                        )}
                      </div>
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
                          onClick={() => setFlowAdv(a)}
                        >
                          余额流水
                        </button>
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
      {flowAdv && (
        <WalletFlowModal advertiser={flowAdv} onClose={() => setFlowAdv(null)} />
      )}
      {rechargeAdv && (
        <RechargeModal advertiser={rechargeAdv} onClose={() => setRechargeAdv(null)} />
      )}
      {confirmGate && (
        <ConfirmDialog
          open
          destructive
          title={`启用钱包闸 · ${confirmGate.name}`}
          confirmLabel="确认启用"
          pending={isPending && gateId === confirmGate.id}
          onClose={() => {
            setConfirmGate(null);
            setGateError(null);
          }}
          onConfirm={() => runToggleGate(confirmGate, true)}
        >
          <p>
            「{confirmGate.name}」当前余额为{" "}
            <span className="font-medium text-foreground">
              {fmtMoney(confirmGate.wallet_balance)}
            </span>
            。启用钱包闸后，余额将作为投放硬顶，会立即停投其下全部广告。
          </p>
          {gateError && <p className="mt-3 font-medium text-destructive">{gateError}</p>}
        </ConfirmDialog>
      )}
    </div>
  );
}
