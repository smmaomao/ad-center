"use client";

// 广告主总钱包面板：余额概览 + 充值 + 流水（充值 + 扣费合并）。
// 总余额是投放硬顶——即使日预算没超，余额耗尽也会停投该广告主下全部广告任务。
import { useActionState, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import type { AdminWallet, WalletFlowRow } from "@/lib/go-api";
import { rechargeWalletAction, type FormState } from "../actions";
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

export function WalletPanel({
  advertiserId,
  wallet,
  flow,
  canWrite,
}: {
  advertiserId: string;
  wallet: AdminWallet;
  flow: WalletFlowRow[];
  canWrite: boolean;
}) {
  const router = useRouter();
  const [state, formAction, pending] = useActionState<FormState, FormData>(
    rechargeWalletAction,
    {},
  );
  const [open, setOpen] = useState(false);

  useEffect(() => {
    if (state.ok) {
      setOpen(false);
      router.refresh();
    }
  }, [state, router]);

  const exhausted = wallet.enabled && wallet.balance <= 0;

  return (
    <Card className={exhausted ? "border-destructive" : undefined}>
      <CardHeader>
        <div className="flex items-start justify-between gap-4">
          <div>
            <CardTitle>账户总钱包</CardTitle>
            <CardDescription>
              总余额是投放硬顶：即使日预算没超，余额耗尽也会停投该广告主下全部广告任务
            </CardDescription>
          </div>
          {canWrite && (
            <Button
              type="button"
              size="sm"
              variant="outline"
              onClick={() => setOpen((v) => !v)}
            >
              {open ? "取消" : "充值"}
            </Button>
          )}
        </div>
      </CardHeader>
      <CardContent className="space-y-5">
        <div className="grid gap-4 sm:grid-cols-3">
          <Stat
            title="当前余额"
            value={`$${wallet.balance.toFixed(2)}`}
            warn={exhausted}
            sub={
              !wallet.enabled
                ? "未启用钱包闸（不受总余额限制）"
                : exhausted
                  ? "余额耗尽 · 已停投"
                  : "正常投放"
            }
          />
          <Stat title="累计充值" value={`$${wallet.deposited.toFixed(2)}`} />
          <Stat title="累计扣费" value={`$${wallet.spent.toFixed(2)}`} />
        </div>

        {open && canWrite && (
          <form
            action={formAction}
            className="grid gap-3 rounded-md border border-border p-4 sm:grid-cols-[1fr_1fr_auto] sm:items-end"
          >
            <input type="hidden" name="advertiser_id" value={advertiserId} />
            <div className="space-y-1.5">
              <Label htmlFor="amount">充值金额（USD）*</Label>
              <Input
                id="amount"
                name="amount"
                type="number"
                step="0.01"
                min="0.01"
                required
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="note">备注</Label>
              <Input id="note" name="note" placeholder="可选" />
            </div>
            <Button type="submit" disabled={pending}>
              {pending ? "入账中…" : "确认充值"}
            </Button>
          </form>
        )}
        {state.error && (
          <p className="text-sm font-medium text-destructive">{state.error}</p>
        )}

        <div>
          <p className="mb-2 text-sm font-medium">流水（充值 + 扣费）</p>
          {flow.length === 0 ? (
            <p className="text-sm text-muted-foreground">暂无流水</p>
          ) : (
            <div className="max-h-80 overflow-auto">
              <table className="w-full text-sm">
                <thead className="sticky top-0 bg-background">
                  <tr className="border-b border-border text-left text-xs text-muted-foreground">
                    <th className="py-2 pr-4 font-medium">时间</th>
                    <th className="py-2 pr-4 font-medium">类型</th>
                    <th className="py-2 pr-4 font-medium">金额</th>
                    <th className="py-2 pr-4 font-medium">说明</th>
                  </tr>
                </thead>
                <tbody>
                  {flow.map((row, i) => (
                    <tr key={i} className="border-b border-border/60">
                      <td className="py-2 pr-4 tabular-nums text-muted-foreground">
                        {row.at}
                      </td>
                      <td className="py-2 pr-4">
                        <span
                          className={`rounded-full px-2 py-0.5 text-xs font-medium ${
                            row.kind === "recharge"
                              ? "bg-emerald-50 text-emerald-700"
                              : row.kind === "adjust"
                                ? "bg-sky-50 text-sky-700"
                                : "bg-amber-50 text-amber-700"
                          }`}
                        >
                          {row.kind === "recharge" ? "充值" : row.kind === "adjust" ? "调账" : "扣费"}
                        </span>
                      </td>
                      <td
                        className={`py-2 pr-4 tabular-nums ${
                          row.kind === "recharge"
                            ? "text-emerald-700"
                            : row.kind === "adjust" && row.amount >= 0
                              ? "text-sky-700"
                              : "text-foreground"
                        }`}
                      >
                        {row.kind === "adjust"
                          ? `${row.amount >= 0 ? "+" : "-"}$${Math.abs(row.amount).toFixed(4)}`
                          : `${row.kind === "recharge" ? "+" : "-"}$${row.amount.toFixed(4)}`}
                      </td>
                      <td className="py-2 pr-4 text-muted-foreground">
                        {row.kind === "recharge"
                          ? row.note || "—"
                          : row.kind === "adjust"
                            ? row.note || "手动调账"
                            : "计费扣费"}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      </CardContent>
    </Card>
  );
}

function Stat({
  title,
  value,
  sub,
  warn,
}: {
  title: string;
  value: string;
  sub?: string;
  warn?: boolean;
}) {
  return (
    <div className="rounded-md border border-border p-3">
      <p className="text-xs text-muted-foreground">{title}</p>
      <p
        className={`text-lg font-semibold tabular-nums ${warn ? "text-red-600" : ""}`}
      >
        {value}
      </p>
      {sub && <p className="text-xs text-muted-foreground">{sub}</p>}
    </div>
  );
}
