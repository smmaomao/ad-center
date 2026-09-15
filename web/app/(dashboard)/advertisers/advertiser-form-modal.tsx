"use client";

import { useRouter } from "next/navigation";
import { Modal } from "@/components/modal";
import { AdvertiserForm } from "./advertiser-form";
import type { AdminAdvertiser } from "@/lib/go-api";

export function AdvertiserFormModal({
  initial,
  canWrite,
  onClose,
}: {
  initial?: AdminAdvertiser;
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
      title={initial ? (canWrite ? "编辑广告主" : "广告主详情") : "新增广告主"}
      description={
        initial
          ? `创建时间：${initial.created_at ?? "—"}`
          : "创建后可在「查看」中补充素材与投放配置"
      }
      onClose={onClose}
      footer={null}
    >
      <AdvertiserForm
        initial={initial}
        modal
        onSaved={handleSaved}
      />
    </Modal>
  );
}
