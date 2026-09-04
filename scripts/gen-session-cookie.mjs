// 生成本地验证用的 @supabase/ssr cookie（base64url 编码，与浏览器登录产物一致）
// 用法: node scripts/gen-session-cookie.mjs > cookie.txt
import { createRequire } from "node:module";
const require = createRequire(
  new URL("../web/", import.meta.url).pathname + "package.json",
);
const { createClient } = require("@supabase/supabase-js");

const AUTH_URL = "http://127.0.0.1:54321";
const KEY = "sb_publishable_ACJWlzQHlZjBrEguHvfOxg_3BJgxAaH";

// ① 密码换 token
const res = await fetch(`${AUTH_URL}/auth/v1/token?grant_type=password`, {
  method: "POST",
  headers: { apikey: KEY, "Content-Type": "application/json" },
  body: JSON.stringify({
    email: "admin@adcenter.test",
    password: "Admin123!pass",
  }),
});
const tokens = await res.json();
if (!tokens.access_token) throw new Error("login failed: " + JSON.stringify(tokens));

// ② 用真实 storage key（sb-<ref>-auth-token）构造 base64url 编码值
const captured = {};
const fakeStorage = {
  getItem: () => null,
  setItem: (k, v) => (captured[k] = v),
  removeItem: () => {},
};
const c = createClient(AUTH_URL, KEY, {
  auth: { storage: fakeStorage, persistSession: true, autoRefreshToken: false },
});
await c.auth.setSession({
  access_token: tokens.access_token,
  refresh_token: tokens.refresh_token,
});
const key = Object.keys(captured).find((k) => k.includes("auth-token"));
if (!key) throw new Error("storage key not captured");

// ③ base64url 编码（@supabase/ssr 默认 cookieEncoding，值带 base64- 前缀）
const b64url = (s) =>
  Buffer.from(s, "utf-8").toString("base64").replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");

console.log(`${key}=base64-${b64url(captured[key])}`);
