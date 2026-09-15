"use client";

import { useActionState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Modal } from "@/components/modal";
import { CopyBox } from "@/components/copy-box";
import { createAppAction, type CreateResult } from "./actions";

export function CreateAppModal({
  canWrite,
  onClose,
}: {
  canWrite: boolean;
  onClose: () => void;
}) {
  const [state, action, pending] = useActionState(createAppAction, {} as CreateResult);

  if (state.id) {
    return (
      <Modal
        open
        title="App 已创建"
        description="请立即复制保存 API Key；之后也可在应用列表 / 编辑页查看脱敏前缀并复制"
        onClose={onClose}
        footer={<Button onClick={onClose}>完成</Button>}
      >
        <div className="space-y-3">
          <div>
            <p className="mb-1 text-xs text-slate-500">Ad_App_Id</p>
            <p className="font-mono text-sm text-slate-900">{state.id}</p>
          </div>
          <div>
            <p className="mb-1 text-xs text-slate-500">API Key（仅此一次显示）</p>
            <CopyBox value={state.api_key ?? ""} />
          </div>
        </div>
      </Modal>
    );
  }

  return (
    <Modal
      open
      title="注册 App"
      description="系统自动生成 Ad_App_Id 与 API Key；创建后可在列表 / 编辑页随时复制"
      onClose={onClose}
      footer={
        canWrite ? (
          <>
            <Button variant="outline" type="button" onClick={onClose}>
              取消
            </Button>
            <Button type="submit" form="create-app-form" disabled={pending}>
              {pending ? "创建中…" : "注册"}
            </Button>
          </>
        ) : (
          <Button type="button" onClick={onClose}>
            关闭
          </Button>
        )
      }
    >
      <form id="create-app-form" action={action} className="space-y-4">
        <div className="space-y-1.5">
          <Label htmlFor="name">应用名称 *</Label>
          <Input id="name" name="name" required placeholder="如 短剧App" autoFocus />
        </div>
        {state.error && <p className="text-sm text-red-600">{state.error}</p>}
      </form>
    </Modal>
  );
}
