"use client";

import { useState } from "react";

/**
 * 脱敏展示密钥：默认显示前缀 + ***，复制按钮复制完整值。
 * 传入 reveal 可临时展示完整明文（如编辑弹窗里的「显示」切换）。
 */
export function MaskedKey({
  value,
  reveal = false,
  copyLabel = "复制",
}: {
  value: string;
  reveal?: boolean;
  copyLabel?: string;
}) {
  const [copied, setCopied] = useState(false);
  const display = reveal || value.length <= 12 ? value : value.slice(0, 12) + "***";

  return (
    <div className="flex items-center gap-2">
      <code className="truncate font-mono text-xs text-slate-700">{display}</code>
      <button
        type="button"
        onClick={() => {
          navigator.clipboard.writeText(value);
          setCopied(true);
          setTimeout(() => setCopied(false), 1500);
        }}
        className="shrink-0 rounded-md bg-white px-2 py-1 text-xs font-medium text-slate-600 shadow-sm ring-1 ring-slate-200 transition hover:text-slate-900"
      >
        {copied ? "已复制" : copyLabel}
      </button>
    </div>
  );
}
