import { NextResponse } from "next/server";

export const dynamic = "force-dynamic";

// 登出 BFF：清除 httpOnly 会话 cookie（令牌本身无状态，清掉即失效）。
export async function POST() {
  const resp = NextResponse.json({ ok: true });
  resp.cookies.set("ad_session", "", { path: "/", maxAge: 0 });
  return resp;
}
