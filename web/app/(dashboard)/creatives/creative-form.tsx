"use client";

// 广告素材表单（三大板块）：
//   ① 基础信息 ② 定向配置（样式 / 投放 App）③ 商业与策略（扣费 / 单价 / 优先级系数）
import { useActionState, useEffect, useState } from "react";
import type { AdminApp, AdminCreative } from "@/lib/go-api";
import { CREATIVE_STYLES } from "@/lib/go-api";
import { saveCreativeAction, type FormState } from "./actions";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

const selectCls =
  "h-8 w-full rounded-lg border border-input bg-transparent px-2 text-sm outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 disabled:opacity-50";

/** 从本地文件解析媒体元数据：视频取宽高+时长，图片取宽高（无时长） */
function probeMedia(
  file: File,
  kind: "video" | "image",
): Promise<{ width: number; height: number; durationMs: number }> {
  return new Promise((resolve) => {
    const url = URL.createObjectURL(file);
    const done = (w: number, h: number, d: number) => {
      URL.revokeObjectURL(url);
      resolve({ width: w, height: h, durationMs: d });
    };
    if (kind === "video") {
      const v = document.createElement("video");
      v.preload = "metadata";
      v.onloadedmetadata = () =>
        done(v.videoWidth, v.videoHeight, Math.round(v.duration * 1000));
      v.onerror = () => done(0, 0, 0);
      v.src = url;
    } else {
      const img = new Image();
      img.onload = () => done(img.naturalWidth, img.naturalHeight, 0);
      img.onerror = () => done(0, 0, 0);
      img.src = url;
    }
  });
}

function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
}

export function CreativeForm({
  apps,
  initial,
  modal,
  onSaved,
}: {
  apps: AdminApp[];
  initial?: AdminCreative;
  modal?: boolean;
  onSaved?: () => void;
}) {
  const [state, formAction, pending] = useActionState<FormState, FormData>(
    saveCreativeAction,
    {},
  );
  const isEdit = Boolean(initial);

  // 上传相关状态（R2 直传）
  const EXT_ALLOWLIST: Record<string, string[]> = {
    video: ["mp4"],
    image: ["png", "jpg", "jpeg", "webp", "gif"],
  };
  const [mediaType, setMediaType] = useState<"video" | "image" | "html">(
    (initial?.media_type as "video" | "image" | "html") ?? "video",
  );
  const [storagePath, setStoragePath] = useState<string>(initial?.storage_path ?? "");
  const [uploading, setUploading] = useState(false);
  const [uploadError, setUploadError] = useState<string | null>(null);
  const [uploadedName, setUploadedName] = useState<string | null>(null);
  // 编辑时直接展示后端返回的 storage_url；新上传时临时用本地 object URL
  const [previewUrl, setPreviewUrl] = useState<string | null>(
    initial?.storage_url ?? null,
  );
  const [meta, setMeta] = useState<{
    fileSizeBytes: number;
    width: number;
    height: number;
    durationMs: number;
  } | null>(null);

  // 卸载时释放预览用 object URL，避免内存泄漏
  useEffect(() => {
    return () => {
      if (previewUrl?.startsWith("blob:")) URL.revokeObjectURL(previewUrl);
    };
  }, [previewUrl]);

  async function uploadFile(file: File) {
    setUploading(true);
    setUploadError(null);
    setUploadedName(null);
    setMeta(null);
    // 本地预览（立即可见，不依赖 R2）
    const localUrl = URL.createObjectURL(file);
    setPreviewUrl((prev) => {
      if (prev?.startsWith("blob:")) URL.revokeObjectURL(prev);
      return localUrl;
    });
    try {
      const ext = (file.name.split(".").pop() ?? "").toLowerCase();
      const allowed = EXT_ALLOWLIST[mediaType] ?? [];
      if (!allowed.includes(ext)) {
        setUploadError(`不支持的扩展名 .${ext}（${mediaType} 仅允许 ${allowed.join(", ")}）`);
        return;
      }
      const buf = await file.arrayBuffer();
      const digest = await crypto.subtle.digest("SHA-256", buf);
      const contentSha256 = Array.from(new Uint8Array(digest))
        .map((b) => b.toString(16).padStart(2, "0"))
        .join("");
      const res = await fetch("/api/uploads/presign", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          media_type: mediaType,
          ext,
          file_size_bytes: file.size,
          content_sha256: contentSha256,
        }),
      });
      const data = await res.json();
      if (!res.ok) {
        setUploadError(data?.error ?? "获取上传凭证失败");
        return;
      }
      const put = await fetch(data.upload_url, {
        method: "PUT",
        body: file,
        headers: { "Content-Type": file.type || "application/octet-stream" },
      });
      if (!put.ok) {
        setUploadError(`直传 R2 失败（${put.status}）`);
        return;
      }
      setStoragePath(data.object_key);
      setUploadedName(file.name);
      // 自动解析媒体元数据（尺寸 / 时长 / 大小）
      const m =
        mediaType === "html"
          ? { width: 0, height: 0, durationMs: 0 }
          : await probeMedia(file, mediaType);
      setMeta({ fileSizeBytes: file.size, ...m });
    } catch (e) {
      setUploadError(e instanceof Error ? e.message : "上传失败");
    } finally {
      setUploading(false);
    }
  }

  // 弹窗内保存成功后，通知外层关闭弹窗
  useEffect(() => {
    if (state.ok) onSaved?.();
  }, [state, onSaved]);

  return (
    <form action={formAction} className="space-y-6">
      {initial && <input type="hidden" name="id" value={initial.id} />}
      {modal && <input type="hidden" name="no_redirect" value="1" />}

      {/* ① 基础信息 */}
      <Card>
        <CardHeader>
          <CardTitle>基础信息</CardTitle>
          <CardDescription>素材名称与素材内容地址（编辑时可直接预览）</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4 sm:grid-cols-2">
          <div className="space-y-1.5">
            <Label htmlFor="name">素材名称 *</Label>
            <Input id="name" name="name" required defaultValue={initial?.name} />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="media_type">媒体类型</Label>
            <select
              id="media_type"
              name="media_type"
              className={selectCls}
              value={mediaType}
              onChange={(e) =>
                setMediaType(e.target.value as "video" | "image" | "html")
              }
            >
              <option value="image">图片（JPG / PNG）</option>
              <option value="video">视频（MP4）</option>
              <option value="html">落地页（URL）</option>
            </select>
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="orientation">尺寸方向</Label>
            <select
              id="orientation"
              name="orientation"
              className={selectCls}
              defaultValue={initial?.orientation ?? "portrait"}
            >
              <option value="portrait">竖屏</option>
              <option value="landscape">横屏</option>
              <option value="any">通用</option>
            </select>
          </div>
          <div className="space-y-1.5 sm:col-span-2">
            <Label htmlFor="storage_path">
              {mediaType === "html" ? "落地页 URL *" : "素材文件 / 地址 *"}
            </Label>
            {mediaType !== "html" && (
              <div className="flex flex-wrap items-center gap-3">
                <input
                  type="file"
                  accept={mediaType === "video" ? "video/*" : "image/*"}
                  disabled={uploading}
                  onChange={(e) => {
                    const f = e.target.files?.[0];
                    if (f) void uploadFile(f);
                  }}
                  className="block w-full max-w-xs text-sm file:mr-3 file:rounded-lg file:border-0 file:bg-brand file:px-3 file:py-1.5 file:text-sm file:text-white hover:file:opacity-90"
                />
                {uploading && (
                  <span className="text-xs text-muted-foreground">上传中…</span>
                )}
                {uploadedName && !uploading && (
                  <span className="text-xs text-emerald-600">
                    已上传：{uploadedName}
                    {meta && (
                      <span className="ml-1 text-emerald-700/80">
                        （{formatBytes(meta.fileSizeBytes)}
                        {meta.width > 0 && ` · ${meta.width}×${meta.height}`}
                        {mediaType === "video" && meta.durationMs > 0
                          ? ` · ${(meta.durationMs / 1000).toFixed(1)}s`
                          : ""}）
                      </span>
                    )}
                  </span>
                )}
                {previewUrl && mediaType === "video" && (
                  <video src={previewUrl} controls className="h-24 rounded-lg" />
                )}
                {previewUrl && mediaType === "image" && (
                  // eslint-disable-next-line @next/next/no-img-element
                  <img src={previewUrl} alt="" className="h-24 rounded-lg" />
                )}
              </div>
            )}
            {uploadError && (
              <p className="text-xs font-medium text-destructive">{uploadError}</p>
            )}
            <Input
              id="storage_path"
              name="storage_path"
              required
              value={storagePath}
              onChange={(e) => setStoragePath(e.target.value)}
              placeholder={
                mediaType === "html"
                  ? "https://..."
                  : "上传后自动填入 object key，也可手动填写 URL"
              }
            />
            {meta && (
              <>
                <input type="hidden" name="file_size_bytes" value={meta.fileSizeBytes} />
                <input type="hidden" name="width" value={meta.width} />
                <input type="hidden" name="height" value={meta.height} />
                <input type="hidden" name="duration_ms" value={meta.durationMs} />
              </>
            )}
            <p className="text-xs text-muted-foreground">
              {mediaType === "html"
                ? "外部落地页链接，不走上传"
                : "选择本地文件后自动计算指纹、直传 R2 并填入 object key（如 creatives/{sha256}.mp4）；也支持手动粘贴 URL"}
            </p>
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="status">状态</Label>
            <select
              id="status"
              name="status"
              className={selectCls}
              defaultValue={initial?.status ?? "active"}
            >
              <option value="active">上架</option>
              <option value="testing">测试中</option>
              <option value="paused">下架</option>
            </select>
          </div>
        </CardContent>
      </Card>

      {/* ② 定向配置 */}
      <Card>
        <CardHeader>
          <CardTitle>定向配置</CardTitle>
          <CardDescription>
            勾选该素材支持的展现样式与要投放的 App
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="space-y-2">
            <Label>展现样式 *（可多选）</Label>
            <div className="flex flex-wrap gap-4">
              {CREATIVE_STYLES.map((s) => (
                <label key={s.key} className="flex items-center gap-2 text-sm">
                  <input
                    type="checkbox"
                    name="styles"
                    value={s.key}
                    defaultChecked={initial?.styles?.includes(s.key)}
                    className="size-4"
                  />
                  {s.label}
                </label>
              ))}
            </div>
          </div>
          <div className="space-y-2">
            <Label>投放目标 App（不选 = 全部 App）</Label>
            <div className="flex flex-wrap gap-4">
              {apps.map((a) => (
                <label key={a.id} className="flex items-center gap-2 text-sm">
                  <input
                    type="checkbox"
                    name="target_apps"
                    value={a.id}
                    defaultChecked={initial?.target_apps?.includes(a.id)}
                    className="size-4"
                  />
                  {a.name}
                </label>
              ))}
            </div>
          </div>
        </CardContent>
      </Card>

      {state.error && (
        <p className="text-sm font-medium text-destructive">{state.error}</p>
      )}
      {state.ok && (
        <p className="text-sm font-medium text-green-600">已保存</p>
      )}
      <Button type="submit" disabled={pending}>
        {pending ? "保存中…" : isEdit ? "保存修改" : "创建素材"}
      </Button>
    </form>
  );
}
