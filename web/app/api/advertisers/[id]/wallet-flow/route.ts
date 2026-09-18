// 广告主余额流水（充值 + 扣费合并，时间倒序）BFF 转发：
// 浏览器 → 本端点（服务端读会话）→ Go /v1/admin/advertisers/{id}/wallet/flow。
// 浏览器不直接连 Go（私网地址 + 缺鉴权），统一走 Next BFF（与 metrics/stream 同思路）。
import { NextRequest, NextResponse } from "next/server";
import { getSession } from "@/lib/auth";
import { listWalletFlow } from "@/lib/go-api";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function GET(
  _req: NextRequest,
  { params }: { params: Promise<{ id: string }> },
) {
  const session = await getSession();
  if (!session) {
    return NextResponse.json({ error: "unauthorized" }, { status: 401 });
  }
  const { id } = await params;
  if (!id) {
    return NextResponse.json({ error: "missing id" }, { status: 400 });
  }

  try {
    const rows = await listWalletFlow(session.email, id);
    return NextResponse.json({ rows });
  } catch (e) {
    const msg = e instanceof Error ? e.message : "加载失败";
    return NextResponse.json({ error: msg }, { status: 502 });
  }
}
