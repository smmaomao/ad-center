"use client";

import { useRouter } from "next/navigation";
import { Modal } from "@/components/modal";
import { CampaignForm } from "./campaign-form";
import type {
  AdminAdvertiser,
  AdminCreative,
  AdminCampaign,
  AdminProduct,
} from "@/lib/go-api";

export function CampaignFormModal({
  advertisers,
  creatives,
  products,
  initial,
  canWrite,
  onClose,
}: {
  advertisers: AdminAdvertiser[];
  creatives: AdminCreative[];
  products: AdminProduct[];
  initial?: AdminCampaign;
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
      title={initial ? (canWrite ? "编辑广告任务" : "广告任务详情") : "新增广告任务"}
      description={
        initial
          ? `任务 ID：${initial.id}`
          : "创建后可在「素材管理」将素材绑定到本任务"
      }
      onClose={onClose}
      footer={null}
    >
      <CampaignForm
        advertisers={advertisers}
        creatives={creatives}
        products={products}
        initial={initial}
        modal
        onSaved={handleSaved}
      />
    </Modal>
  );
}
