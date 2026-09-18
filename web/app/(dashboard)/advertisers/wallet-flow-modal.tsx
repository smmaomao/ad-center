"use client";

import { useEffect, useState } from "react";
import { Modal } from "@/components/modal";
import type { AdminAdvertiser, WalletFlowRow } from "@/lib/go-api";

function fmtMoney(n: number): string {
  return "$" + (n ?? 0).toLocaleString("en-US", {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  });
}

const KIND_LABEL: Record<string, string> = {
  recharge: "充值",
  deduct: "扣费",
  adjust: "调账",
};

// 各流水类型的徽标配色。
const KIND_BADGE: Record<string, string> = {
  recharge: "bg-emerald-500/15 text-emerald-300",
  deduct: "bg-rose-500/15 text-rose-300",
  adjust: "bg-sky-500/15 text-sky-300",
};

const OP_LABEL: Record<string, string> = {
  deduct_agg: "消费聚合",
  deduct: "消费明细",
};

export function WalletFlowModal({
  advertiser,
  onClose,
}: {
  advertiser: AdminAdvertiser;
  onClose: () => void;
}) {
  const [rows, setRows] = useState<WalletFlowRow[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let alive = true;
    setLoading(true);
    setError(null);
    setRows(null);
    fetch(`/api/advertisers/${advertiser.id}/wallet-flow`, { cache: "no-store" })
      .then(async (r) => {
        const body = (await r.json().catch(() => ({}))) as {
          rows?: WalletFlowRow[];
          error?: string;
        };
        if (!r.ok) throw new Error(body.error ?? "加载失败");
        if (!alive) return;
        setRows(body.rows ?? []);
      })
      .catch((e: unknown) => {
        if (!alive) return;
        setError(e instanceof Error ? e.message : "加载失败");
      })
      .finally(() => {
        if (alive) setLoading(false);
      });
    return () => {
      alive = false;
    };
  }, [advertiser.id]);

  return (
    <Modal
      open
      size="lg"
      title={`${advertiser.name} · 余额流水`}
      description={
        advertiser.wallet_enabled
          ? `当前余额 ${fmtMoney(advertiser.wallet_balance)}`
          : "钱包闸未启用（存量广告主，不受总余额限制）"
      }
      onClose={onClose}
      footer={<button type="button" onClick={onClose} className="text-sm text-muted-foreground hover:underline">关闭</button>}
    >
      <div className="space-y-3">
        {!advertiser.wallet_enabled && (
          <div className="rounded-lg border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-xs text-amber-300">
            该广告主尚未启用总钱包闸，余额不参与投放硬顶控制。
          </div>
        )}

        {loading && <p className="py-6 text-center text-sm text-muted-foreground">加载中…</p>}

        {error && (
          <div className="rounded-lg border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
            加载失败：{error}
          </div>
        )}

        {!loading && !error && rows && rows.length === 0 && (
          <p className="py-6 text-center text-sm text-muted-foreground">暂无流水</p>
        )}

        {!loading && !error && rows && rows.length > 0 && (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead className="text-left text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                <tr className="border-b border-border">
                  <th className="px-3 py-2">类型</th>
                  <th className="px-3 py-2 text-right">金额</th>
                  <th className="px-3 py-2">币种</th>
                  <th className="px-3 py-2">细分</th>
                  <th className="px-3 py-2">备注/操作人</th>
                  <th className="px-3 py-2">时间</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((r, i) => (
                  <tr key={i} className="border-b border-border/60 last:border-0">
                    <td className="px-3 py-2">
                      <span
                        className={`rounded-full px-2 py-0.5 text-xs ${
                          KIND_BADGE[r.kind] ?? "bg-muted text-muted-foreground"
                        }`}
                      >
                        {KIND_LABEL[r.kind] ?? r.kind}
                      </span>
                    </td>
                    <td className="px-3 py-2 text-right tabular-nums">
                      {r.kind === "adjust"
                        ? `${r.amount >= 0 ? "+" : "−"}${fmtMoney(Math.abs(r.amount))}`
                        : `${r.kind === "recharge" ? "+" : "−"}${fmtMoney(r.amount)}`}
                    </td>
                    <td className="px-3 py-2 text-muted-foreground">{r.currency || "USD"}</td>
                    <td className="px-3 py-2 text-muted-foreground">
                      {r.op_type ? OP_LABEL[r.op_type] ?? r.op_type : "—"}
                    </td>
                    <td className="px-3 py-2 text-muted-foreground">
                      {r.note || r.created_by || "—"}
                    </td>
                    <td className="px-3 py-2 tabular-nums text-muted-foreground">{r.at}</td>
                  </tr>
                ))}
              </tbody>
            </table>
            <p className="mt-2 text-xs text-muted-foreground/70">仅显示最近 200 条</p>
          </div>
        )}
      </div>
    </Modal>
  );
}
