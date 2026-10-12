"use client";

import { useEffect, useState } from "react";
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

const REDIRECT_ERRORS: Record<string, string> = {
  no_access: "登录已失效或无权限，请重新登录",
  session_expired: "会话已过期，请重新登录",
};

export default function LoginPage() {
  const [redirectError, setRedirectError] = useState<string | null>(null);

  // 刻意不用 useSearchParams：它在未包 <Suspense> 时会让整页 CSR bailout、
  // React 放弃 hydrate（表现就是登录按钮点了没反应、且没有任何提示）。
  // 改为挂载后读一次 location.search，SSR 与首帧渲染一致，不破坏 hydration。
  useEffect(() => {
    const q = new URLSearchParams(window.location.search).get("error");
    if (q) setRedirectError(q);
  }, []);
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  async function handleSubmit(e: React.SyntheticEvent<HTMLFormElement>) {
    e.preventDefault();
    setError(null);
    setLoading(true);

    // 只把账号密码交给后端登录接口，前端不接触任何 Supabase。
    try {
      const res = await fetch("/api/auth/login", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email, password }),
      });

      if (!res.ok) {
        setError("邮箱或密码错误");
        setLoading(false);
        return;
      }

      // 整页跳转：确保刚下发的 ad_session cookie 一定随 /dashboard 请求带上，
      // 避免客户端 router.push 在 HMR 失效 / 旧 bundle / cookie 提交时序下的边界问题。
      window.location.assign("/dashboard");
    } catch {
      // 请求本身失败（网络/代理拦截）时也要给出提示，而不是静默卡住。
      setError("登录请求失败：无法连接服务，请检查网络或代理设置后重试");
      setLoading(false);
    }
  }

  return (
    <main className="flex min-h-screen items-center justify-center bg-muted/40">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle className="text-2xl">BeReal Media</CardTitle>
          <CardDescription>广告管理系统</CardDescription>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleSubmit} className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="email">邮箱</Label>
              <Input
                id="email"
                type="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                placeholder="you@example.com"
                required
                autoFocus
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="password">密码</Label>
              <Input
                id="password"
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                required
              />
            </div>
            {(error || redirectError) && (
              <p className="text-sm text-destructive">
                {error ?? REDIRECT_ERRORS[redirectError as string] ?? "登录失败，请重试"}
              </p>
            )}
            <Button type="submit" className="w-full" disabled={loading}>
              {loading ? "登录中…" : "登录"}
            </Button>
          </form>
        </CardContent>
      </Card>
    </main>
  );
}
