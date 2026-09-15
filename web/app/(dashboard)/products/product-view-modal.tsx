"use client";

import type { ReactNode } from "react";
import { Modal } from "@/components/modal";
import { Button } from "@/components/ui/button";
import type { AdminProduct } from "@/lib/go-api";

function Field({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="space-y-1">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="text-sm font-medium text-foreground">{value}</dd>
    </div>
  );
}

export function ProductViewModal({
  product,
  advertiserName,
  onClose,
}: {
  product: AdminProduct;
  advertiserName: string;
  onClose: () => void;
}) {
  const status = product.status;
  return (
    <Modal
      open
      size="lg"
      title={product.name}
      description={`产品 ID：${product.id}`}
      onClose={onClose}
      footer={<Button variant="outline" onClick={onClose}>关闭</Button>}
    >
      <div className="space-y-6">
        <div className="flex items-center gap-2">
          <span
            className={`inline-flex rounded-full px-2 py-0.5 text-xs font-medium ${
              status === "active"
                ? "bg-emerald-50 text-emerald-700"
                : "bg-zinc-100 text-zinc-600"
            }`}
          >
            {status === "active" ? "启用" : "已暂停"}
          </span>
          <span className="text-sm text-muted-foreground">
            所属广告主：{advertiserName}
          </span>
        </div>

        <section>
          <h3 className="mb-3 text-xs font-semibold uppercase tracking-wider text-muted-foreground">
            产品信息
          </h3>
          <dl className="grid gap-x-6 gap-y-4 sm:grid-cols-2">
            <Field label="产品名称" value={product.name} />
            <Field
              label="日预算"
              value={product.daily_budget ? `$${product.daily_budget}` : "不限"}
            />
            <Field label="备注" value={product.notes || "—"} />
          </dl>
        </section>
      </div>
    </Modal>
  );
}
