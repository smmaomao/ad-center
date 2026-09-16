import { NextResponse } from "next/server";
import { GO_API_URL, INTERNAL_API_KEY } from "@/lib/go-api";

export const dynamic = "force-dynamic";

// 后端登录 BFF：把账号密码交给 Go /v1/admin/login，把返回的会话令牌写入
// httpOnly cookie（前端 JS 读不到，避免 XSS 窃取）。前端不接触 Supabase。
export async function POST(req: Request) {
  let body: { email?: string; password?: string };
  try {
    body = await req.json();
  } catch {
    return NextResponse.json({ error: "bad request" }, { status: 400 });
  }
  if (!body.email || !body.password) {
    return NextResponse.json({ error: "email and password required" }, { status: 400 });
  }

  let data: { token?: string; error?: string };
  try {
    const res = await fetch(GO_API_URL + "/v1/admin/login", {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-Internal-Key": INTERNAL_API_KEY },
      body: JSON.stringify({ email: body.email, password: body.password }),
      cache: "no-store",
    });
    data = await res.json().catch(() => ({}));
    if (!res.ok) {
      return NextResponse.json({ error: data.error ?? "登录失败" }, { status: res.status });
    }
  } catch {
    return NextResponse.json({ error: "登录服务不可用" }, { status: 502 });
  }

  if (!data.token) {
    return NextResponse.json({ error: "no token returned" }, { status: 500 });
  }

  const resp = NextResponse.json({ ok: true });
  resp.cookies.set("ad_session", data.token, {
    httpOnly: true,
    sameSite: "lax",
    secure: process.env.NODE_ENV === "production",
    path: "/",
    maxAge: 60 * 60 * 24 * 7,
  });
  return resp;
}
