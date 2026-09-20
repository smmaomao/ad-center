// 看板实时流代理（已暂停使用）：浏览器 → BFF（校验会话）→ Go /v1/admin/metrics/stream。
// Go 端当前返回 503 + 暂停说明（不推送 SSE），本端点原样透传，便于未来恢复。
import { NextRequest } from "next/server";
import { getSession } from "@/lib/auth";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function GET(req: NextRequest) {
  const session = await getSession();
  if (!session) {
    return new Response("unauthorized", { status: 401 });
  }

  const goUrl =
    (process.env.GO_API_URL ?? "http://127.0.0.1:8888") +
    "/v1/admin/metrics/stream";
  const key = process.env.INTERNAL_API_KEY ?? "";
  if (!key) {
    return new Response("INTERNAL_API_KEY not configured", { status: 500 });
  }

  let upstream: Response;
  try {
    upstream = await fetch(goUrl, {
      method: "GET",
      headers: {
        "X-Internal-Key": key,
        "X-Actor-Email": session.email,
        Accept: "text/event-stream",
      },
      cache: "no-store",
      signal: req.signal,
    });
  } catch {
    return new Response("upstream unavailable", { status: 502 });
  }

  // 透传上游响应（含暂停时的 503 + JSON 说明）
  return new Response(upstream.body, {
    status: upstream.status,
    headers: {
      "Content-Type": upstream.headers.get("Content-Type") ?? "application/json",
      "Cache-Control": "no-cache, no-transform",
    },
  });
}
