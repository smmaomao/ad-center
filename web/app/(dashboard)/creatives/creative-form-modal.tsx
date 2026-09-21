"use client";

import { useRouter } from "next/navigation";
import { Modal } from "@/components/modal";
import { CreativeForm } from "./creative-form";
import type { AdminApp, AdminCreative } from "@/lib/go-api";

export function CreativeFormModal({
  apps,
  initial,
  canWrite,
  onClose,
}: {
  apps: AdminApp[];
  initial?: AdminCreative;
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
      title={initial ? (canWrite ? "编辑素材" : "素材详情") : "新建素材"}
      description={
        initial
          ? `素材 ID：${initial.id}`
          : "注册素材后，在广告任务中按需绑定"
      }
      onClose={onClose}
      footer={null}
    >
      <CreativeForm
        apps={apps}
        initial={initial}
        modal
        onSaved={handleSaved}
      />
    </Modal>
  );
}
