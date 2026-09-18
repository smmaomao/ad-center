"use client";

// 广告主钱包弹窗：两种视图
//  - 充值：同事务写充值流水 + 递增余额 + 置 wallet_enabled=true（余额作为投放硬顶）
//  - 修改：手动调账，修正余额并写一条调账流水（不改钱包闸开关，不并入累计充值）
import { useActionState, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { Modal } from "@/components/modal";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import type { AdminAdvertiser } from "@/lib/go-api";
import {
  rechargeWalletAction,
  adjustWalletAction,
  type FormState,
} from "./actions";

function fmtMoney(n: number): string {
  return "$" + (n ?? 0).toLocaleString("en-US", {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  });
}

export function RechargeModal({
  advertiser,
  onClose,
}: {
  advertiser: AdminAdvertiser;
  onClose: () => void;
}) {
  const router = useRouter();
  const [view, setView] = useState<"recharge" | "adjust">("recharge");
  const [adjustMode, setAdjustMode] = useState<"set" | "delta">("set");

  const [rechargeState, rechargeAction, rechargePending] = useActionState<
    FormState,
    FormData
  >(rechargeWalletAction, {});
  const [adjustState, adjustAction, adjustPending] = useActionState<
    FormState,
    FormData
  >(adjustWalletAction, {});

  useEffect(() => {
    if (rechargeState.ok || adjustState.ok) {
      onClose();
      router.refresh();
    }
  }, [rechargeState, adjustState, onClose, router]);

  const isRecharge = view === "recharge";
  const current = fmtMoney(advertiser.wallet_balance);

  return (
    <Modal
      open
      size="md"
      title={isRecharge ? `充值 · ${advertiser.name}` : `修改余额 · ${advertiser.name}`}
      description={
        isRecharge
          ? advertiser.wallet_enabled
            ? `当前余额 ${current}`
            : "首次充值将同时启用总钱包闸（余额成为投放硬顶）"
          : `当前余额 ${current}；调账只改余额并写一条调账流水，不影响充值记录与闸门开关`
      }
      onClose={onClose}
      footer={null}
    >
      {isRecharge ? (
        <form action={rechargeAction} className="space-y-5">
          <input type="hidden" name="advertiser_id" value={advertiser.id} />

          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label htmlFor="recharge-amount">充值金额（USD）*</Label>
              <Input
                id="recharge-amount"
                name="amount"
                type="number"
                step="0.01"
                min="0.01"
                placeholder="例如 1000.00"
                required
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="recharge-note">备注</Label>
              <Input id="recharge-note" name="note" placeholder="可选" />
            </div>
          </div>

          {rechargeState.error && (
            <p className="text-sm font-medium text-destructive">{rechargeState.error}</p>
          )}

          <div className="flex justify-end gap-2">
            <Button
              type="button"
              variant="secondary"
              onClick={() => setView("adjust")}
              disabled={rechargePending}
            >
              修改
            </Button>
            <Button type="button" variant="outline" onClick={onClose} disabled={rechargePending}>
              取消
            </Button>
            <Button type="submit" disabled={rechargePending}>
              {rechargePending ? "入账中…" : "确认充值"}
            </Button>
          </div>
        </form>
      ) : (
        <form action={adjustAction} className="space-y-5">
          <input type="hidden" name="advertiser_id" value={advertiser.id} />
          <input type="hidden" name="mode" value={adjustMode} />

          <div className="space-y-2">
            <Label>调整方式</Label>
            <div className="flex gap-2">
              <button
                type="button"
                onClick={() => setAdjustMode("set")}
                className={`flex-1 rounded-lg border px-3 py-2 text-sm transition ${
                  adjustMode === "set"
                    ? "border-brand bg-brand/10 font-medium text-brand"
                    : "border-border text-muted-foreground hover:bg-muted"
                }`}
              >
                设为指定余额
              </button>
              <button
                type="button"
                onClick={() => setAdjustMode("delta")}
                className={`flex-1 rounded-lg border px-3 py-2 text-sm transition ${
                  adjustMode === "delta"
                    ? "border-brand bg-brand/10 font-medium text-brand"
                    : "border-border text-muted-foreground hover:bg-muted"
                }`}
              >
                增减金额（可负）
              </button>
            </div>
          </div>

          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label htmlFor="adjust-amount">
                {adjustMode === "set" ? "目标余额（USD）*" : "增减金额（USD）*"}
              </Label>
              <Input
                id="adjust-amount"
                name="amount"
                type="number"
                step="0.01"
                min={adjustMode === "set" ? "0" : undefined}
                placeholder={adjustMode === "set" ? "例如 500.00" : "正数增加 / 负数减少"}
                required
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="adjust-note">备注</Label>
              <Input id="adjust-note" name="note" placeholder="如 冲正 / 补差" />
            </div>
          </div>

          <div className="rounded-lg border border-amber-500/40 bg-amber-500/10 px-3 py-2.5 text-xs text-amber-600 dark:text-amber-300">
            调账不会改变钱包闸开关，也不计入累计充值；调整后余额不能为负。
          </div>

          {adjustState.error && (
            <p className="text-sm font-medium text-destructive">{adjustState.error}</p>
          )}

          <div className="flex justify-end gap-2">
            <Button
              type="button"
              variant="ghost"
              onClick={() => setView("recharge")}
              disabled={adjustPending}
            >
              返回充值
            </Button>
            <Button type="submit" disabled={adjustPending}>
              {adjustPending ? "调整中…" : "确认调整"}
            </Button>
          </div>
        </form>
      )}
    </Modal>
  );
}
