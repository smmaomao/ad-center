"use client";

import { useActionState, useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Modal } from "@/components/modal";
import { CopyBox } from "@/components/copy-box";
import { MaskedKey } from "@/components/masked-key";
import {
  updateAppAction,
  resetAppSecretAction,
  resetAppKeyAction,
  type UpdateResult,
  type ResetResult,
  type ResetKeyResult,
} from "./actions";
import type { AdminApp } from "@/lib/go-api";

const selectCls =
  "h-9 w-full rounded-lg border border-slate-200 bg-white px-3 text-sm outline-none transition focus-visible:border-slate-400 focus-visible:ring-2 focus-visible:ring-slate-200 disabled:opacity-60";

export function EditAppModal({
  app,
  canWrite,
  onClose,
}: {
  app: AdminApp;
  canWrite: boolean;
  onClose: () => void;
}) {
  const [uState, uAction, uPending] = useActionState(updateAppAction, {} as UpdateResult);
  const [rState, rAction, rPending] = useActionState(resetAppSecretAction, {} as ResetResult);
  const [kState, kAction, kPending] = useActionState(resetAppKeyAction, {} as ResetKeyResult);
  const [showKey, setShowKey] = useState(false);

  const footer = canWrite ? (
    <>
      <Button variant="outline" type="button" onClick={onClose}>
        关闭
      </Button>
      <Button type="submit" form="edit-app-form" disabled={uPending}>
        {uPending ? "保存中…" : "保存"}
      </Button>
    </>
  ) : (
    <Button type="button" onClick={onClose}>
      关闭
    </Button>
  );

  return (
    <Modal
      open
      title={canWrite ? "编辑应用" : "应用详情"}
      description={`Ad_App_Id：${app.id}`}
      onClose={onClose}
      footer={footer}
    >
      <div className="space-y-5">
        <form id="edit-app-form" action={uAction} className="space-y-4">
          <input type="hidden" name="id" value={app.id} />
          <div className="space-y-1.5">
            <Label htmlFor="name">应用名称 *</Label>
            <Input id="name" name="name" defaultValue={app.name} disabled={!canWrite} />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="callback_url">业务回调地址（S2S）</Label>
            <Input
              id="callback_url"
              name="callback_url"
              placeholder="https://xiuxian.com"
              defaultValue={app.callback_url ?? ""}
              disabled={!canWrite}
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="status">状态</Label>
            <select
              id="status"
              name="status"
              className={selectCls}
              defaultValue={app.status}
              disabled={!canWrite}
            >
              <option value="active">启用</option>
              <option value="paused">暂停广告下发</option>
            </select>
          </div>
          {uState.error && <p className="text-sm text-red-600">{uState.error}</p>}
          {uState.ok && <p className="text-sm text-emerald-600">已保存</p>}
        </form>

        <div className="border-t border-slate-100 pt-4">
          <div className="mb-2 flex items-start justify-between gap-3">
            <div>
              <p className="text-sm font-medium text-slate-900">S2S 签名密钥</p>
              <p className="text-xs text-slate-500">
                用于业务后端校验回调节点签名，请勿泄露
              </p>
            </div>
            {canWrite && (
              <form action={rAction}>
                <input type="hidden" name="id" value={app.id} />
                <Button
                  type="submit"
                  variant="outline"
                  size="sm"
                  disabled={rPending}
                  onClick={(e) => {
                    if (!confirm("重置后旧密钥立即失效，业务后端需同步更新，确定继续？")) {
                      e.preventDefault();
                    }
                  }}
                >
                  {rPending ? "重置中…" : "重置密钥"}
                </Button>
              </form>
            )}
          </div>
          {rState.secret ? (
            <div className="space-y-1">
              <p className="text-xs text-amber-600">
                密钥已重置，旧密钥立即失效，请同步更新业务后端：
              </p>
              <CopyBox value={rState.secret} />
            </div>
          ) : (
            <p className="font-mono text-xs text-slate-400">
              {app.secret_key || "—"}
            </p>
          )}
          {rState.error && (
            <p className="mt-1 text-sm text-red-600">{rState.error}</p>
          )}
        </div>

        <div className="border-t border-slate-100 pt-4">
          <div className="mb-2 flex items-start justify-between gap-3">
            <div>
              <p className="text-sm font-medium text-slate-900">API Key</p>
              <p className="text-xs text-slate-500">
                客户端调用广告接口的身份凭证，请勿泄露
              </p>
            </div>
            {canWrite && (
              <form action={kAction}>
                <input type="hidden" name="id" value={app.id} />
                <Button
                  type="submit"
                  variant="outline"
                  size="sm"
                  disabled={kPending}
                  onClick={(e) => {
                    if (
                      !confirm(
                        "重置后旧 API Key 立即失效，客户端需同步更新，确定继续？",
                      )
                    ) {
                      e.preventDefault();
                    }
                  }}
                >
                  {kPending ? "重置中…" : "重置 Key"}
                </Button>
              </form>
            )}
          </div>
          {kState.key ? (
            <div className="space-y-1">
              <p className="text-xs text-amber-600">
                API Key 已重置，旧 Key 立即失效，请同步更新客户端：
              </p>
              <CopyBox value={kState.key} />
            </div>
          ) : app.api_key ? (
            <div className="flex items-center justify-between gap-3">
              <div className="min-w-0 flex-1">
                <MaskedKey value={app.api_key} reveal={showKey} />
              </div>
              <button
                type="button"
                onClick={() => setShowKey((v) => !v)}
                className="shrink-0 text-xs font-medium text-slate-500 hover:text-slate-900"
              >
                {showKey ? "隐藏" : "显示"}
              </button>
            </div>
          ) : (
            <p className="font-mono text-xs text-slate-400">—</p>
          )}
          {kState.error && (
            <p className="mt-1 text-sm text-red-600">{kState.error}</p>
          )}
        </div>
      </div>
    </Modal>
  );
}
