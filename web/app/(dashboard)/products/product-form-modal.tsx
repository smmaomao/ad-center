"use client";

import { useRouter } from "next/navigation";
import { Modal } from "@/components/modal";
import { ProductForm } from "./product-form";
import type { AdminAdvertiser, AdminProduct } from "@/lib/go-api";

export function ProductFormModal({
  advertisers,
  initial,
  canWrite,
  onClose,
}: {
  advertisers: AdminAdvertiser[];
  initial?: AdminProduct;
  canWrite: boolean;
  onClose: () => void;
}) {
  const router = useRouter();
  const handleSaved = () => {
    onClose();
    router.refresh();
  };

  return (
    <Modal
      open
      size="lg"
      title={initial ? (canWrite ? "编辑产品" : "产品详情") : "新增产品"}
      description={
        initial
          ? `产品 ID：${initial.id}`
          : "创建后可在「广告任务管理」中将任务绑定到本产品"
      }
      onClose={onClose}
      footer={null}
    >
      <ProductForm
        advertisers={advertisers}
        initial={initial}
        modal
        onSaved={handleSaved}
      />
    </Modal>
  );
}
