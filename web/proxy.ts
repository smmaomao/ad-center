import { NextResponse, type NextRequest } from "next/server";

// 会话守卫（Next 16 中 middleware 已更名为 proxy）：
// 仅按会话 cookie 是否存在做粗粒度拦截，真正的身份校验在页面/接口层（Go 验签令牌）。
// 不做令牌验签，避免 edge runtime 引入 crypto 依赖；无效 cookie 会在进入页面时
// 被 getSession() 判空并跳转 /login。
export async function proxy(request: NextRequest) {
  const token = request.cookies.get("ad_session")?.value;
  const { pathname } = request.nextUrl;

  // 登录 / 登出是公开的认证入口，放行（否则无 token 的 /api/ 请求会被 401 挡掉）
  if (pathname.startsWith("/api/auth/")) {
    return NextResponse.next({ request });
  }

  if (!token && !pathname.startsWith("/login")) {
    // API 路由返回 JSON 401（fetch 调用方拿到结构化错误，而非登录页 HTML）
    if (pathname.startsWith("/api/")) {
      return NextResponse.json({ error: "unauthorized" }, { status: 401 });
    }
    const url = request.nextUrl.clone();
    url.pathname = "/login";
    return NextResponse.redirect(url);
  }

  if (token && pathname === "/login") {
    const url = request.nextUrl.clone();
    url.pathname = "/dashboard";
    return NextResponse.redirect(url);
  }

  return NextResponse.next({ request });
}

export const config = {
  matcher: [
    // 排除静态资源，其余全走会话守卫
    "/((?!_next/static|_next/image|favicon.ico|.*\\.(?:svg|png|jpg|jpeg|gif|webp|ico)$).*)",
  ],
};
