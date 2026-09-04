// R2（S3 兼容）预签名签名器 —— 与 Go internal/storage/signer.go 同源算法
// （ARCHITECTURE.md §2.7：写侧 presigned PUT 由 BFF 生成，浏览器直传 R2，
// 不经 Go 服务；写凭证只存在于 Next.js 服务端环境变量，不进 Go 进程、不下发浏览器）。
// 纯 node:crypto 实现零依赖；对象 key 字符集受 regex 严格限制（无 SigV4 边界情况）。
import { createHash, createHmac } from "node:crypto";

/** R2 固定 region（SigV4 格式要求，R2 不校验） */
const REGION = "auto";

export interface R2Config {
  accountId: string;
  bucket: string;
  accessKeyId: string;
  secretAccessKey: string;
}

/** 从环境变量读取 R2 配置；任一缺失返回 null（调用方应返回 503） */
export function r2ConfigFromEnv(): R2Config | null {
  const c = process.env;
  if (!c.R2_ACCOUNT_ID || !c.R2_BUCKET || !c.R2_ACCESS_KEY_ID || !c.R2_SECRET_ACCESS_KEY) {
    return null;
  }
  return {
    accountId: c.R2_ACCOUNT_ID,
    bucket: c.R2_BUCKET,
    accessKeyId: c.R2_ACCESS_KEY_ID,
    secretAccessKey: c.R2_SECRET_ACCESS_KEY,
  };
}

/**
 * 生成对象上传的预签名 PUT URL（R2 path-style：/{bucket}/{key}，bucket 必须
 * 进签名路径）。短时效（建议 10 分钟）：浏览器拿到后立即直传。
 */
export function presignPUT(cfg: R2Config, objectKey: string, expiresSec: number): string {
  const host = `${cfg.accountId}.r2.cloudflarestorage.com`;
  return presignURL(
    host,
    cfg.accessKeyId,
    cfg.secretAccessKey,
    REGION,
    "PUT",
    `${cfg.bucket}/${objectKey}`,
    expiresSec,
    amzNow(),
  );
}

/** 当前 UTC 时间的 AmzDate 格式（yyyyMMddTHHmmssZ） */
function amzNow(): string {
  return new Date().toISOString().replace(/[-:]/g, "").replace(/\.\d{3}/, "");
}

// ============================================================
// 以下为 SigV4 查询串签名核心，与 Go 版逐行对拍（向量测试见 signer.test.ts）
// ============================================================

/**
 * SigV4 查询串签名（host/amzDate 可注入，供 AWS 官方测试向量对拍）。
 * objectKey 为完整签名路径（path-style 下含 bucket 前缀）。
 */
export function presignURL(
  host: string,
  accessKeyId: string,
  secretAccessKey: string,
  region: string,
  method: "GET" | "PUT",
  objectKey: string,
  expiresSec: number,
  amzDate: string,
): string {
  const date = amzDate.slice(0, 8);
  const scope = `${date}/${region}/s3/aws4_request`;

  const params: [string, string][] = [
    ["X-Amz-Algorithm", "AWS4-HMAC-SHA256"],
    ["X-Amz-Credential", `${accessKeyId}/${scope}`],
    ["X-Amz-Date", amzDate],
    ["X-Amz-Expires", String(expiresSec)],
    ["X-Amz-SignedHeaders", "host"],
  ];
  const canonicalQuery = canonicalQueryEncode(params);

  const canonicalRequest = [
    method,
    canonicalURI(objectKey),
    canonicalQuery,
    `host:${host}`,
    "", // CanonicalHeaders 后的空行
    "host",
    "UNSIGNED-PAYLOAD",
  ].join("\n");

  const stringToSign = [
    "AWS4-HMAC-SHA256",
    amzDate,
    scope,
    sha256Hex(canonicalRequest),
  ].join("\n");

  const signature = hex(
    hmac(signingKey(secretAccessKey, date, region, "s3"), stringToSign),
  );

  return `https://${host}${canonicalURI(objectKey)}?${canonicalQuery}&X-Amz-Signature=${signature}`;
}

/** S3 规范路径：逐段 RFC3986 编码（空格 = %20，非 +），/ 保留 */
function canonicalURI(key: string): string {
  const segs = key.replace(/^\/+|\/+$/g, "").split("/");
  return "/" + segs.map(awsEncode).join("/");
}

/** AWS 规范查询串：按键排序，键值均严格编码（值中的 / → %2F） */
function canonicalQueryEncode(params: [string, string][]): string {
  return [...params]
    .sort((a, b) => (a[0] < b[0] ? -1 : a[0] > b[0] ? 1 : 0))
    .map(([k, v]) => `${awsEncode(k)}=${awsEncode(v)}`)
    .join("&");
}

/** RFC 3986 未保留字符集之外全部百分号编码（按字节，多字节字符逐字节编码） */
function awsEncode(s: string): string {
  const bytes = new TextEncoder().encode(s);
  let out = "";
  for (const b of bytes) {
    const c = String.fromCharCode(b);
    if (/[A-Za-z0-9\-._~]/.test(c)) {
      out += c;
    } else {
      out += "%" + b.toString(16).toUpperCase().padStart(2, "0");
    }
  }
  return out;
}

function signingKey(secret: string, date: string, region: string, service: string): Buffer {
  const kDate = hmac("AWS4" + secret, date);
  const kRegion = hmac(kDate, region);
  const kService = hmac(kRegion, service);
  return hmac(kService, "aws4_request");
}

function hmac(key: string | Buffer, data: string): Buffer {
  return createHmac("sha256", key).update(data).digest();
}

function sha256Hex(data: string): string {
  return createHash("sha256").update(data).digest("hex");
}

function hex(b: Buffer): string {
  return b.toString("hex");
}
