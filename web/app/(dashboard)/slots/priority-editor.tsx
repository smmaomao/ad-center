"use client";

// 填充优先级编辑器（PRD FR-04）：
// HTML5 拖拽 + 上下移按钮调整顺序，保存时全量替换（position = 数组下标）。
// 保量份额总和 ≤ 1 由前端预校验，Go 侧仍复核。
import { useState, useTransition } from "react";
import { useRouter } from "next/navigation";
import type { AdminPriority, PriorityInput } from "@/lib/go-api";
import { savePrioritiesAction } from "./actions";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

interface Row {
  key: string;
  source_type: AdminPriority["source_type"];
  advertiser_id: string;
  expected_ecpm: string;
  guaranteed_share: string;
  weight: string;
  enabled: boolean;
}

function toRows(ps: AdminPriority[]): Row[] {
  return ps.map((p, i) => ({
    key: p.id || `row-${i}`,
    source_type: p.source_type,
    advertiser_id: p.advertiser_id ?? "",
    expected_ecpm: p.expected_ecpm > 0 ? String(p.expected_ecpm) : "",
    guaranteed_share: p.guaranteed_share > 0 ? String(p.guaranteed_share) : "",
    weight: p.weight > 0 ? String(p.weight) : "",
    enabled: p.enabled,
  }));
}

let rowSeq = 0;
function newRow(): Row {
  return {
    key: `new-${++rowSeq}`,
    source_type: "advertiser",
    advertiser_id: "",
    expected_ecpm: "",
    guaranteed_share: "",
    weight: "",
    enabled: true,
  };
}

const cellCls = "h-7 rounded-md border border-input bg-transparent px-1.5 text-xs";
const selectCls = `${cellCls} w-full`;

export function PriorityEditor({
  slotId,
  initial,
  advertisers,
}: {
  slotId: string;
  initial: AdminPriority[];
  advertisers: { id: string; name: string }[];
}) {
  const router = useRouter();
  const [rows, setRows] = useState<Row[]>(() => toRows(initial));
  const [dragIndex, setDragIndex] = useState<number | null>(null);
  const [overIndex, setOverIndex] = useState<number | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);
  const [pending, startTransition] = useTransition();

  const guaranteedSum = rows.reduce(
    (acc, r) => acc + (Number.parseFloat(r.guaranteed_share) || 0),
    0,
  );
  const sumExceeded = guaranteedSum > 1.0001;

  function update(i: number, patch: Partial<Row>) {
    setSaved(false);
    setRows((rs) => rs.map((r, j) => (j === i ? { ...r, ...patch } : r)));
  }

  function move(from: number, to: number) {
    setSaved(false);
    setRows((rs) => {
      if (to < 0 || to >= rs.length || from === to) return rs;
      const next = [...rs];
      const [row] = next.splice(from, 1);
      next.splice(to, 0, row);
      return next;
    });
  }

  function addRow() {
    setSaved(false);
    setRows((rs) => [...rs, newRow()]);
  }

  function removeRow(i: number) {
    setSaved(false);
    setRows((rs) => rs.filter((_, j) => j !== i));
  }

  function save() {
    setError(null);
    if (sumExceeded) {
      setError("保量份额总和超过 1，请调整后再保存");
      return;
    }
    const advertiserIds = new Set(advertisers.map((a) => a.id));
    const payload: PriorityInput[] = rows.map((r) => ({
      source_type: r.source_type,
      ...(r.source_type === "advertiser"
        ? {
            advertiser_id: r.advertiser_id,
            ...(Number.parseFloat(r.expected_ecpm) > 0
              ? { expected_ecpm: Number.parseFloat(r.expected_ecpm) }
              : {}),
          }
        : {}),
      ...(Number.parseFloat(r.guaranteed_share) > 0
        ? { guaranteed_share: Number.parseFloat(r.guaranteed_share) }
        : {}),
      ...(Number.parseFloat(r.weight) > 0
        ? { weight: Number.parseFloat(r.weight) }
        : {}),
      enabled: r.enabled,
    }));
    for (const p of payload) {
      if (p.source_type === "advertiser" && (!p.advertiser_id || !advertiserIds.has(p.advertiser_id))) {
        setError("来源类型为「广告主」的行必须选择广告主");
        return;
      }
    }
    startTransition(async () => {
      const res = await savePrioritiesAction(slotId, payload);
      if (res.error) {
        setError(res.error);
        return;
      }
      setSaved(true);
      router.refresh();
    });
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>填充优先级</CardTitle>
        <CardDescription>
          从上到下依次尝试填充；拖拽行或使用 ↑↓ 调整顺序，保存时全量替换
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">
        {rows.length === 0 ? (
          <p className="py-4 text-center text-sm text-muted-foreground">
            还没有填充来源，添加第一行（advertiser = 指定广告主 · max = 最高 eCPM · fallback = 兜底）
          </p>
        ) : (
          <div className="overflow-x-auto rounded-lg border">
            <table className="w-full min-w-3xl text-sm">
              <thead>
                <tr className="border-b bg-muted/40 text-left text-xs text-muted-foreground">
                  <th className="w-24 px-3 py-2 font-medium">顺序</th>
                  <th className="px-3 py-2 font-medium">来源类型</th>
                  <th className="px-3 py-2 font-medium">广告主</th>
                  <th className="px-3 py-2 font-medium">预期 eCPM</th>
                  <th className="hidden px-3 py-2 font-medium">保量份额</th>
                  <th className="px-3 py-2 font-medium">权重</th>
                  <th className="px-3 py-2 font-medium">启用</th>
                  <th className="w-12 px-3 py-2 font-medium"></th>
                </tr>
              </thead>
              <tbody>
                {rows.map((r, i) => (
                  <tr
                    key={r.key}
                    draggable
                    onDragStart={(e) => {
                      setDragIndex(i);
                      e.dataTransfer.effectAllowed = "move";
                    }}
                    onDragOver={(e) => {
                      e.preventDefault();
                      setOverIndex(i);
                    }}
                    onDrop={(e) => {
                      e.preventDefault();
                      if (dragIndex !== null) move(dragIndex, i);
                      setDragIndex(null);
                      setOverIndex(null);
                    }}
                    onDragEnd={() => {
                      setDragIndex(null);
                      setOverIndex(null);
                    }}
                    className={`border-b last:border-0 ${
                      dragIndex === i
                        ? "opacity-40"
                        : overIndex === i
                          ? "bg-violet-50"
                          : ""
                    }`}
                  >
                    <td className="px-3 py-2">
                      <div className="flex items-center gap-1">
                        <span
                          className="cursor-grab text-muted-foreground select-none"
                          title="拖拽排序"
                        >
                          ⠿
                        </span>
                        <span className="tabular-nums text-xs">{i + 1}</span>
                        <div className="flex flex-col">
                          <button
                            type="button"
                            aria-label="上移"
                            className="px-1 text-[10px] leading-3 text-muted-foreground hover:text-foreground disabled:opacity-30"
                            disabled={i === 0}
                            onClick={() => move(i, i - 1)}
                          >
                            ▲
                          </button>
                          <button
                            type="button"
                            aria-label="下移"
                            className="px-1 text-[10px] leading-3 text-muted-foreground hover:text-foreground disabled:opacity-30"
                            disabled={i === rows.length - 1}
                            onClick={() => move(i, i + 1)}
                          >
                            ▼
                          </button>
                        </div>
                      </div>
                    </td>
                    <td className="px-3 py-2">
                      <select
                        value={r.source_type}
                        onChange={(e) =>
                          update(i, {
                            source_type: e.target
                              .value as Row["source_type"],
                          })
                        }
                        className={selectCls}
                      >
                        <option value="advertiser">指定广告主</option>
                        <option value="max">最高 eCPM</option>
                        <option value="fallback">兜底</option>
                      </select>
                    </td>
                    <td className="px-3 py-2">
                      <select
                        value={r.advertiser_id}
                        onChange={(e) => update(i, { advertiser_id: e.target.value })}
                        className={selectCls}
                        disabled={r.source_type !== "advertiser"}
                      >
                        <option value="">
                          {r.source_type === "advertiser" ? "选择广告主" : "—"}
                        </option>
                        {advertisers.map((a) => (
                          <option key={a.id} value={a.id}>
                            {a.name}
                          </option>
                        ))}
                      </select>
                    </td>
                    <td className="px-3 py-2">
                      <Input
                        type="number"
                        step="0.01"
                        min="0"
                        className="h-7 w-20 px-1.5 text-xs"
                        value={r.expected_ecpm}
                        onChange={(e) => update(i, { expected_ecpm: e.target.value })}
                        disabled={r.source_type !== "advertiser"}
                      />
                    </td>
                    <td className="hidden px-3 py-2">
                      <Input
                        type="number"
                        step="0.01"
                        min="0"
                        max="1"
                        className="h-7 w-20 px-1.5 text-xs"
                        value={r.guaranteed_share}
                        onChange={(e) => update(i, { guaranteed_share: e.target.value })}
                      />
                    </td>
                    <td className="px-3 py-2">
                      <Input
                        type="number"
                        step="0.1"
                        min="0"
                        className="h-7 w-20 px-1.5 text-xs"
                        value={r.weight}
                        onChange={(e) => update(i, { weight: e.target.value })}
                      />
                    </td>
                    <td className="px-3 py-2">
                      <input
                        type="checkbox"
                        checked={r.enabled}
                        onChange={(e) => update(i, { enabled: e.target.checked })}
                        className="size-4"
                      />
                    </td>
                    <td className="px-3 py-2 text-right">
                      <button
                        type="button"
                        aria-label="删除行"
                        className="text-muted-foreground hover:text-destructive"
                        onClick={() => removeRow(i)}
                      >
                        ✕
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}

        <div className="flex flex-wrap items-center gap-3">
          <Button variant="outline" size="sm" onClick={addRow}>
            + 添加来源
          </Button>
          <span
            className={`hidden text-xs tabular-nums ${sumExceeded ? "font-medium text-red-600" : "text-muted-foreground"}`}
          >
            保量份额总和：{guaranteedSum.toFixed(2)} / 1.00
          </span>
          <div className="ml-auto flex items-center gap-3">
            {error && <span className="text-xs font-medium text-destructive">{error}</span>}
            {saved && !error && <span className="text-xs text-emerald-700">已保存</span>}
            <Button size="sm" onClick={save} disabled={pending}>
              {pending ? "保存中…" : "保存优先级"}
            </Button>
          </div>
        </div>
      </CardContent>
    </Card>
  );
}
