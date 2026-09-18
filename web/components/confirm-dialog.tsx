"use client";

// 通用二次确认弹窗：替代 window.confirm，统一走项目内 Modal 样式（含背景遮罩、
// Esc 关闭、滚动锁）。用于删除 / 高风险操作等需要用户明确确认的场景。
import type { ReactNode } from "react";
import { Modal } from "@/components/modal";
import { Button } from "@/components/ui/button";

export function ConfirmDialog({
  open,
  title,
  description,
  confirmLabel = "确定",
  cancelLabel = "取消",
  destructive = false,
  pending = false,
  onConfirm,
  onClose,
  children,
}: {
  open: boolean;
  title: string;
  description?: string;
  confirmLabel?: string;
  cancelLabel?: string;
  destructive?: boolean;
  pending?: boolean;
  onConfirm: () => void;
  onClose: () => void;
  children?: ReactNode;
}) {
  return (
    <Modal
      open={open}
      size="sm"
      title={title}
      description={description}
      onClose={onClose}
      footer={null}
    >
      <div className="space-y-5">
        {children && <div className="text-sm text-muted-foreground">{children}</div>}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="outline" onClick={onClose} disabled={pending}>
            {cancelLabel}
          </Button>
          <Button
            type="button"
            variant={destructive ? "destructive" : "default"}
            onClick={onConfirm}
            disabled={pending}
          >
            {pending ? "处理中…" : confirmLabel}
          </Button>
        </div>
      </div>
    </Modal>
  );
}
