// R2 直传预签名端点（ARCHITECTURE.md §2.7 上传链路）：
// 浏览器 → BFF（本端点，校验会话/角色/参数，生成 presigned PUT）→ 浏览器持 URL
// 直传 R2 → 浏览器携 object_key 调 Go 管理 API 注册素材元数据。
// 写凭证只在本服务端使用，不进响应。
import { NextRequest, NextResponse } from "next/server";
import { getSession } from "@/lib/auth";
import { goApi } from "@/lib/go-api";
import { presignPUT, r2ConfigFromEnv } from "@/lib/r2/signer";

/** 上传角色：与 Go 侧素材创建一致（operator 及以上） */
const UPLOAD_ROLES = ["super_admin", "operator"];

/** 媒体类型 → 扩展名白名单（html 为外链素材，不走上传） */
const EXT_ALLOWLIST: Record<string, string[]> = {
  video: ["mp4"],
  image: ["png", "jpg", "jpeg", "webp", "gif"],
};

const MAX_FILE_BYTES = 100 * 1024 * 1024; // 100MB
const UPLOAD_TTL_SECONDS = 600; // 预签名时效 10 分钟

const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
const SHA256_RE = /^[0-9a-f]{64}$/;

interface PresignRequest {
  advertiser_id: string;
  media_type: string;
  ext: string;
  file_size_bytes: number;
  /** 文件内容 SHA-256（hex 64 位），前端读取文件后计算传入，用于内容寻址命名（去重 + 稳定 URL） */
  content_sha256: string;
}

export async function POST(req: NextRequest) {
  // 会话 + 角色双重校验（Go 注册侧仍会复核 RBAC）
  const session = await getSession();
  if (!session || !UPLOAD_ROLES.includes(session.role)) {
    return NextResponse.json({ error: "forbidden" }, { status: 403 });
  }

  const cfg = r2ConfigFromEnv();
  if (!cfg) {
    return NextResponse.json({ error: "R2 not configured" }, { status: 503 });
  }

  const body = (await req.json().catch(() => null)) as PresignRequest | null;
  // advertiser_id：空 = 公共素材库（不绑定广告主）；非空须为合法 id。
  // 随广告主 id 迁移为 bigint，故接受整数 id（兼容旧 uuid 形态）。
  const advId = body?.advertiser_id ?? "";
  const idValid = advId === "" || /^[0-9]+$/.test(advId) || UUID_RE.test(advId);
  if (!body || !idValid) {
    return NextResponse.json({ error: "invalid advertiser_id" }, { status: 400 });
  }
  const exts = EXT_ALLOWLIST[body.media_type];
  const ext = (body.ext ?? "").toLowerCase();
  if (!exts || !exts.includes(ext)) {
    return NextResponse.json(
      { error: `unsupported ext for ${body.media_type}: ${ext}` },
      { status: 400 },
    );
  }
  if (!Number.isInteger(body.file_size_bytes) || body.file_size_bytes <= 0 || body.file_size_bytes > MAX_FILE_BYTES) {
    return NextResponse.json({ error: `file_size_bytes must be 1..${MAX_FILE_BYTES}` }, { status: 400 });
  }
  if (!SHA256_RE.test(body.content_sha256 ?? "")) {
    return NextResponse.json(
      { error: "invalid content_sha256 (need 64-hex SHA-256 of file bytes)" },
      { status: 400 },
    );
  }

  // 广告主存在性校验（复用 Go API，防孤儿对象）；公共素材库（空 id）跳过
  if (advId !== "") {
    try {
      await goApi(`/v1/admin/advertisers/${advId}`, session.email);
    } catch {
      return NextResponse.json({ error: "advertiser not found" }, { status: 404 });
    }
  }

  // 对象 key：creatives/{内容SHA-256}.{ext}
  // 扁平内容寻址（content-addressed）命名：相同素材天然去重、URL 稳定、跨广告主共享同一条对象，
  // 配合 CDN 长缓存（immutable）可最大化边缘命中率（见 ARCHITECTURE.md §2.7）。
  const objectKey = `creatives/${body.content_sha256}.${ext}`;

  return NextResponse.json({
    upload_url: presignPUT(cfg, objectKey, UPLOAD_TTL_SECONDS),
    object_key: objectKey,
    expires_in: UPLOAD_TTL_SECONDS,
  });
}
