"use client";

import { useEffect, useState } from "react";
import type { ReactNode } from "react";
import { Modal } from "@/components/modal";
import { Button } from "@/components/ui/button";
import { CREATIVE_STYLES, type AdminCreative } from "@/lib/go-api";
import { getCreativePlayUrlAction } from "./actions";

const STATUS_LABEL: Record<string, string> = {
  active: "上架",
  testing: "测试中",
  paused: "下架",
};

function formatBytes(n: number): string {
  if (!n || n < 0) return "—";
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
}
function formatDuration(ms: number): string {
  if (!ms || ms <= 0) return "—";
  const s = ms / 1000;
  if (s < 60) return `${s.toFixed(1)}s`;
  const m = Math.floor(s / 60);
  const rem = Math.round(s % 60);
  return `${m}m${rem}s`;
}

function Field({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="space-y-1">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="text-sm font-medium text-foreground">{value}</dd>
    </div>
  );
}

// 预览加载状态：loading / ok(url,mediaType) / error(message)
type PlayState =
  | { status: "loading" }
  | { status: "ok"; url: string; mediaType: string }
  | { status: "error"; message: string };

function Preview({ play, storagePath }: { play: PlayState; storagePath: string }) {
  const [failed, setFailed] = useState(false);
  useEffect(() => setFailed(false), [play]);

  if (play.status === "loading") {
    return (
      <div className="flex h-48 items-center justify-center rounded-lg bg-muted/40 text-sm text-muted-foreground">
        正在生成播放地址…
      </div>
    );
  }
  if (play.status === "error") {
    return (
      <div className="rounded-lg border border-destructive/40 bg-destructive/5 p-4 text-sm text-destructive">
        无法生成播放地址：{play.message}
        <div className="mt-1 break-all font-mono text-xs text-muted-foreground">
          {storagePath}
        </div>
      </div>
    );
  }
  if (failed) {
    return (
      <div className="rounded-lg border border-destructive/40 bg-destructive/5 p-4 text-sm text-destructive">
        素材资源加载失败，文件可能不存在于 R2。
        <div className="mt-1 break-all font-mono text-xs text-muted-foreground">
          {storagePath}
        </div>
      </div>
    );
  }

  const { url, mediaType } = play;
  if (mediaType === "image") {
    // eslint-disable-next-line @next/next/no-img-element
    return (
      <img
        src={url}
        alt="creative"
        onError={() => setFailed(true)}
        className="max-h-96 w-full rounded-lg bg-black/5 object-contain"
      />
    );
  }
  if (mediaType === "html") {
    return (
      <iframe
        src={url}
        title="creative"
        sandbox="allow-scripts allow-same-origin"
        className="h-80 w-full rounded-lg border bg-white"
      />
    );
  }
  // video（默认）：controls 直接可点播
  return (
    <video
      src={url}
      controls
      preload="metadata"
      onError={() => setFailed(true)}
      className="max-h-96 w-full rounded-lg bg-black"
    />
  );
}

export function CreativeViewModal({
  creative,
  advertiserName,
  appName,
  onClose,
}: {
  creative: AdminCreative;
  advertiserName?: string;
  appName: (id: string) => string;
  onClose: () => void;
}) {
  const status = creative.status;
  const styleLabel = (k: string) =>
    CREATIVE_STYLES.find((s) => s.key === k)?.label ?? k;

  const [play, setPlay] = useState<PlayState>({ status: "loading" });
  useEffect(() => {
    let cancelled = false;
    setPlay({ status: "loading" });
    getCreativePlayUrlAction(creative.id)
      .then((r) => {
        if (!cancelled) setPlay({ status: "ok", url: r.url, mediaType: r.media_type });
      })
      .catch((e: unknown) => {
        if (!cancelled)
          setPlay({ status: "error", message: e instanceof Error ? e.message : "获取播放地址失败" });
      });
    return () => {
      cancelled = true;
    };
  }, [creative.id]);

  return (
    <Modal
      open
      size="lg"
      title={creative.name}
      description={`素材 ID：${creative.id}`}
      onClose={onClose}
      footer={<Button variant="outline" onClick={onClose}>关闭</Button>}
    >
      <div className="space-y-6">
        <div className="flex items-center gap-2">
          <span
            className={`inline-flex rounded-full px-2 py-0.5 text-xs font-medium ${
              status === "active"
                ? "bg-emerald-50 text-emerald-700"
                : status === "testing"
                ? "bg-amber-50 text-amber-700"
                : "bg-zinc-100 text-zinc-600"
            }`}
          >
            {STATUS_LABEL[status] ?? status}
          </span>
          <span className="text-sm text-muted-foreground">
            归属：{advertiserName ?? "公共素材库"}
          </span>
        </div>

        <section>
          <h3 className="mb-3 text-xs font-semibold uppercase tracking-wider text-muted-foreground">
            基础信息
          </h3>
          <dl className="grid gap-x-6 gap-y-4 sm:grid-cols-2">
            <Field label="媒体类型" value={creative.media_type} />
            <Field label="尺寸方向" value={creative.orientation} />
            <Field label="文件大小" value={formatBytes(creative.file_size_bytes)} />
            {creative.width > 0 && (
              <Field label="分辨率" value={`${creative.width}×${creative.height}`} />
            )}
            {creative.media_type === "video" && creative.duration_ms > 0 && (
              <Field label="时长" value={formatDuration(creative.duration_ms)} />
            )}
            <Field
              label="素材地址"
              value={
                <span className="break-all font-mono text-xs">
                  {creative.storage_path}
                </span>
              }
            />
            <Field label="状态" value={STATUS_LABEL[status] ?? status} />
          </dl>
        </section>

        <section>
          <h3 className="mb-3 text-xs font-semibold uppercase tracking-wider text-muted-foreground">
            素材预览
          </h3>
          <Preview play={play} storagePath={creative.storage_path} />
        </section>

        <section>
          <h3 className="mb-3 text-xs font-semibold uppercase tracking-wider text-muted-foreground">
            投放定向
          </h3>
          <dl className="grid gap-x-6 gap-y-4 sm:grid-cols-2">
            <Field
              label="展现样式"
              value={
                (creative.styles ?? []).map(styleLabel).join("、") || "—"
              }
            />
            <Field
              label="投放 App"
              value={
                (creative.target_apps ?? []).length === 0
                  ? "全部 App"
                  : (creative.target_apps ?? []).map(appName).join("、")
              }
            />
          </dl>
        </section>
      </div>
    </Modal>
  );
}
