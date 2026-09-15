import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  /* config options here */

  // 关掉页面上那个可拖动的 N 图标（Next.js DevTools 指示器）。
  // 它只在 next dev 下出现，生产构建本来就没有；关掉它不影响编译报错浮层。
  // 想挪位置而不隐藏的话：devIndicators: { position: "bottom-right" }
  devIndicators: false,
};

export default nextConfig;
