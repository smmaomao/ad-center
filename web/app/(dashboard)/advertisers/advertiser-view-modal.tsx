"use client";

import { Modal } from "@/components/modal";
import { Button } from "@/components/ui/button";
import type { AdminAdvertiser } from "@/lib/go-api";

function Stat({ label, value, sub, warn }: { label: string; value: string; sub?: string; warn?: boolean }) {
  return (
    <div className="rounded-xl border border-border bg-muted/30 p-3">
      <p className="text-xs text-muted-foreground">{label}</p>
      <p className={`mt-0.5 text-lg font-semibold tabular-nums ${warn ? "text-red-600" : "text-foreground"}`}>
        {value}
      </p>
      {sub && <p className="mt-0.5 text-xs text-muted-foreground">{sub}</p>}
    </div>
  );
}

export function AdvertiserViewModal({
  advertiser,
  onClose,
}: {
  advertiser: AdminAdvertiser;
  onClose: () => void;
}) {
  const a = advertiser;

  return (
    <Modal
      open
      size="lg"
      title={a.name}
      description={a.created_at ? `创建时间：${a.created_at}` : "创建时间：—"}
      onClose={onClose}
      footer={<Button variant="outline" onClick={onClose}>关闭</Button>}
    >
      <div className="space-y-5">
        <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
          {a.contact && <span>联系方式：{a.contact}</span>}
        </div>

        <div className="rounded-xl border border-border bg-muted/30 p-3">
          <p className="text-xs text-muted-foreground">备注</p>
          <p className="mt-0.5 whitespace-pre-wrap text-sm text-foreground">
            {a.notes || "—"}
          </p>
        </div>
      </div>
    </Modal>
  );
}
