// 看板 SSE 代理（阶段 3.1）：浏览器 → BFF（本端点，校验会话）→ Go /v1/admin/metrics/stream。
// 浏览器用 EventSource 同源连 /api/metrics/stream；本端点把 Go 的 SSE 流原样透传，
// 并随客户端断开（req.signal）自动中止上游连接，避免孤儿连接。
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
      signal: req.signal, // 客户端断开即中止上游，释放连接
    });
  } catch {
    return new Response("upstream unavailable", { status: 502 });
  }

  if (!upstream.ok || !upstream.body) {
    return new Response("upstream error", { status: 502 });
  }

  return new Response(upstream.body, {
    status: 200,
    headers: {
      "Content-Type": "text/event-stream",
      "Cache-Control": "no-cache, no-transform",
      Connection: "keep-alive",
      "X-Accel-Buffering": "no",
    },
  });
}
