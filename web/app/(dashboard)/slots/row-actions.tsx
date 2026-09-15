"use client";

// 广告位列表操作列：查看（详情）/ 修改（详情内编辑表单锚点）/ 删除（软删）。
import Link from "next/link";
import { useTransition } from "react";
import { deleteSlotAction } from "./actions";

export function SlotRowActions({
  id,
  name,
  canDelete = true,
}: {
  id: string;
  name: string;
  canDelete?: boolean;
}) {
  const [pending, startTransition] = useTransition();

  return (
    <div className="flex justify-end gap-3 text-xs">
      <Link
        href={`/slots/${id}`}
        className="text-muted-foreground hover:underline"
      >
        查看
      </Link>
      <Link
        href={`/slots/${id}#edit`}
        className="text-muted-foreground hover:underline"
      >
        修改
      </Link>
      {canDelete && (
        <button
          type="button"
          disabled={pending}
          className="text-destructive hover:underline disabled:opacity-50"
          onClick={() => {
            if (
              !window.confirm(
                `确定删除广告位「${name}」？删除后不再参与填充（软删，历史数据保留）。`,
              )
            ) {
              return;
            }
            startTransition(async () => {
              await deleteSlotAction(id);
            });
          }}
        >
          {pending ? "删除中…" : "删除"}
        </button>
      )}
    </div>
  );
}
