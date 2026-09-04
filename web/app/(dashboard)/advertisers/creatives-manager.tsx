"use client";

// 素材管理（PRD 4.2）：R2 直传（presign → 浏览器 PUT → 注册元数据）、
// html 外链登记、状态/权重调整与软删。列表数据来自服务端 props，
// 变更后 revalidatePath + router.refresh() 自动刷新。
import { useRef, useState, useTransition } from "react";
import { useRouter } from "next/navigation";
import type { AdminCreative } from "@/lib/go-api";
import {
  registerCreativeAction,
  updateCreativeAction,
  deleteCreativeAction,
} from "./actions";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

const MAX_FILE_BYTES = 100 * 1024 * 1024;
const VIDEO_EXTS = ["mp4"];
const IMAGE_EXTS = ["png", "jpg", "jpeg", "webp", "gif"];

const MEDIA_LABEL: Record<AdminCreative["media_type"], string> = {
  video: "视频",
  image: "图片",
  html: "HTML",
};

const CREATABLE_STATUS = ["active", "testing", "paused"] as const;
const STATUS_LABEL: Record<string, string> = {
  active: "投放中",
  testing: "测试中",
  paused: "已暂停",
};

function fmtSize(bytes: number): string {
  if (bytes >= 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
  if (bytes >= 1024) return `${(bytes / 1024).toFixed(0)} KB`;
  return `${bytes} B`;
}

export function CreativesManager({
  advertiserId,
  creatives,
}: {
  advertiserId: string;
  creatives: AdminCreative[];
}) {
  const router = useRouter();
  const fileRef = useRef<HTMLInputElement>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [, startTransition] = useTransition();
  // html 外链表单
  const [htmlName, setHtmlName] = useState("");
  const [htmlUrl, setHtmlUrl] = useState("");

  function refresh() {
    startTransition(() => router.refresh());
  }

  async function handleUpload(file: File) {
    setError(null);
    const ext = file.name.split(".").pop()?.toLowerCase() ?? "";
    const mediaType = VIDEO_EXTS.includes(ext)
      ? "video"
      : IMAGE_EXTS.includes(ext)
        ? "image"
        : null;
    if (!mediaType) {
      setError("仅支持 mp4 视频 / png·jpg·jpeg·webp·gif 图片");
      return;
    }
    if (file.size > MAX_FILE_BYTES) {
      setError("文件超过 100MB 上限");
      return;
    }
    setBusy(`正在上传 ${file.name}…`);
    try {
      // 1. BFF 预签名（会话/角色/白名单校验在服务端）
      const presign = (await fetch("/api/uploads/presign", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          advertiser_id: advertiserId,
          media_type: mediaType,
          ext,
          file_size_bytes: file.size,
        }),
      }).then((r) => r.json())) as {
        upload_url?: string;
        object_key?: string;
        error?: string;
      };
      if (!presign.upload_url || !presign.object_key) {
        throw new Error(presign.error ?? "预签名失败");
      }
      // 2. 浏览器直传 R2
      const put = await fetch(presign.upload_url, {
        method: "PUT",
        body: file,
        headers: { "Content-Type": file.type || "application/octet-stream" },
      });
      if (!put.ok) {
        throw new Error(`对象存储直传失败（HTTP ${put.status}）`);
      }
      // 3. 注册素材元数据
      const res = await registerCreativeAction({
        advertiser_id: advertiserId,
        name: file.name.replace(/\.[^.]+$/, ""),
        media_type: mediaType,
        storage_path: presign.object_key,
        file_size_bytes: file.size,
      });
      if (res.error) throw new Error(res.error);
      refresh();
    } catch (e) {
      setError(e instanceof Error ? e.message : "上传失败");
    } finally {
      setBusy(null);
      if (fileRef.current) fileRef.current.value = "";
    }
  }

  async function addHtmlCreative() {
    setError(null);
    if (!htmlName.trim() || !/^https?:\/\//.test(htmlUrl.trim())) {
      setError("需要名称与 http(s) 外链 URL");
      return;
    }
    setBusy("正在登记 HTML 素材…");
    try {
      const res = await registerCreativeAction({
        advertiser_id: advertiserId,
        name: htmlName.trim(),
        media_type: "html",
        storage_path: htmlUrl.trim(),
        file_size_bytes: 0,
      });
      if (res.error) throw new Error(res.error);
      setHtmlName("");
      setHtmlUrl("");
      refresh();
    } catch (e) {
      setError(e instanceof Error ? e.message : "登记失败");
    } finally {
      setBusy(null);
    }
  }

  async function patch(creative: AdminCreative, fields: Record<string, unknown>) {
    setError(null);
    const res = await updateCreativeAction(creative.id, advertiserId, fields);
    if (res.error) setError(res.error);
    else refresh();
  }

  async function remove(creative: AdminCreative) {
    if (!confirm(`确认删除素材「${creative.name}」？`)) return;
    setError(null);
    const res = await deleteCreativeAction(creative.id, advertiserId);
    if (res.error) setError(res.error);
    else refresh();
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>素材管理</CardTitle>
        <CardDescription>
          视频/图片直传对象存储（≤100MB），HTML 素材登记外链
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="flex flex-wrap items-center gap-2">
          <input
            ref={fileRef}
            type="file"
            accept=".mp4,.png,.jpg,.jpeg,.webp,.gif"
            className="hidden"
            onChange={(e) => {
              const f = e.target.files?.[0];
              if (f) void handleUpload(f);
            }}
          />
          <Button onClick={() => fileRef.current?.click()} disabled={busy !== null}>
            上传视频 / 图片
          </Button>
          <Input
            className="w-40"
            placeholder="HTML 素材名称"
            value={htmlName}
            onChange={(e) => setHtmlName(e.target.value)}
            disabled={busy !== null}
          />
          <Input
            className="w-72"
            placeholder="https:// 外链 URL"
            value={htmlUrl}
            onChange={(e) => setHtmlUrl(e.target.value)}
            disabled={busy !== null}
          />
          <Button
            variant="outline"
            onClick={() => void addHtmlCreative()}
            disabled={busy !== null}
          >
            登记外链
          </Button>
          {busy && <span className="text-sm text-muted-foreground">{busy}</span>}
        </div>

        {error && <p className="text-sm font-medium text-destructive">{error}</p>}

        {creatives.length === 0 ? (
          <p className="py-6 text-center text-sm text-muted-foreground">
            还没有素材，先上传一个
          </p>
        ) : (
          <div className="overflow-x-auto rounded-lg border">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b bg-muted/40 text-left text-muted-foreground">
                  <th className="px-3 py-2 font-medium">名称</th>
                  <th className="px-3 py-2 font-medium">类型</th>
                  <th className="px-3 py-2 font-medium">规格</th>
                  <th className="px-3 py-2 font-medium">状态</th>
                  <th className="px-3 py-2 font-medium">权重</th>
                  <th className="px-3 py-2 font-medium text-right">操作</th>
                </tr>
              </thead>
              <tbody>
                {creatives.map((c) => (
                  <tr key={c.id} className="border-b last:border-0">
                    <td className="max-w-52 truncate px-3 py-2" title={c.storage_path}>
                      {c.name}
                    </td>
                    <td className="px-3 py-2">{MEDIA_LABEL[c.media_type]}</td>
                    <td className="px-3 py-2 text-muted-foreground">
                      {c.media_type === "html"
                        ? "外链"
                        : [
                            c.width > 0 ? `${c.width}×${c.height}` : null,
                            c.duration_ms > 0 ? `${(c.duration_ms / 1000).toFixed(1)}s` : null,
                            c.file_size_bytes > 0 ? fmtSize(c.file_size_bytes) : null,
                          ]
                            .filter(Boolean)
                            .join(" · ") || "—"}
                    </td>
                    <td className="px-3 py-2">
                      <select
                        value={c.status}
                        onChange={(e) => void patch(c, { status: e.target.value })}
                        className="h-7 rounded-md border border-input bg-transparent px-1.5 text-xs"
                      >
                        {CREATABLE_STATUS.map((s) => (
                          <option key={s} value={s}>
                            {STATUS_LABEL[s]}
                          </option>
                        ))}
                      </select>
                    </td>
                    <td className="px-3 py-2">
                      <input
                        type="number"
                        step="0.1"
                        min="0"
                        defaultValue={c.weight || ""}
                        className="h-7 w-16 rounded-md border border-input bg-transparent px-1.5 text-xs tabular-nums"
                        onBlur={(e) => {
                          const w = Number.parseFloat(e.target.value);
                          if (Number.isFinite(w) && w > 0 && w !== c.weight) {
                            void patch(c, { weight: w });
                          }
                        }}
                      />
                    </td>
                    <td className="px-3 py-2 text-right">
                      <Button
                        variant="destructive"
                        size="xs"
                        onClick={() => void remove(c)}
                      >
                        删除
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
