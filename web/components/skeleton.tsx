import * as React from "react";

import { cn } from "@/lib/utils";

/**
 * 骨架屏基础块。
 *
 * 用途：配合 App Router 的 loading.tsx —— 切菜单时立刻渲染占位结构，
 * 消除"点了没反应"的卡顿观感。真正的耗时在服务端：page.tsx 是 async
 * Server Component，需串行打 Go 拿身份/菜单 + 拿列表数据，期间旧页面
 * 会一直保持不动；有了 loading 边界，导航瞬间就有反馈。
 */
export function Skeleton({ className }: { className?: string }) {
  return (
    <div
      aria-hidden
      className={cn("animate-pulse rounded-md bg-muted", className)}
    />
  );
}

/** 页面标题骨架：与 components/page-header 的结构对齐（标题 + 描述 + 右侧操作）。 */
export function PageHeaderSkeleton({
  withActions = true,
}: {
  withActions?: boolean;
}) {
  return (
    <div className="flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between">
      <div className="min-w-0 space-y-2">
        <Skeleton className="h-7 w-40" />
        <Skeleton className="h-4 w-64 max-w-full" />
      </div>
      {withActions && (
        <div className="flex shrink-0 items-center gap-2">
          <Skeleton className="h-9 w-24" />
          <Skeleton className="h-9 w-28" />
        </div>
      )}
    </div>
  );
}

/** 卡片容器骨架（与 ui/card 外观一致：圆角 + 细边框）。 */
export function CardSkeleton({
  className,
  children,
}: {
  className?: string;
  children?: React.ReactNode;
}) {
  return (
    <div
      className={cn(
        "rounded-xl bg-card p-4 ring-1 ring-foreground/10",
        className,
      )}
    >
      {children}
    </div>
  );
}

/** 表格骨架：表头一行 + rows 行，每行 cols 列。 */
export function TableSkeleton({
  rows = 6,
  cols = 5,
  className,
}: {
  rows?: number;
  cols?: number;
  className?: string;
}) {
  return (
    <CardSkeleton className={cn("p-0", className)}>
      <div className="flex items-center gap-4 border-b border-foreground/10 px-4 py-3">
        {Array.from({ length: cols }).map((_, i) => (
          <Skeleton
            key={`h-${i}`}
            className={cn("h-3.5", i === 0 ? "w-[28%]" : "flex-1")}
          />
        ))}
      </div>
      <div className="divide-y divide-foreground/10">
        {Array.from({ length: rows }).map((_, r) => (
          <div key={`r-${r}`} className="flex items-center gap-4 px-4 py-3.5">
            {Array.from({ length: cols }).map((_, c) => (
              <Skeleton
                key={`c-${r}-${c}`}
                className={cn("h-4", c === 0 ? "w-[28%]" : "flex-1")}
              />
            ))}
          </div>
        ))}
      </div>
    </CardSkeleton>
  );
}

/** KPI 数字卡骨架（dashboard 顶部四宫格）。 */
export function StatCardsSkeleton({ count = 4 }: { count?: number }) {
  return (
    <div className="grid grid-cols-2 gap-4 md:grid-cols-4">
      {Array.from({ length: count }).map((_, i) => (
        <CardSkeleton key={i} className="space-y-3">
          <Skeleton className="h-3.5 w-20" />
          <Skeleton className="h-7 w-24" />
          <Skeleton className="h-3 w-16" />
        </CardSkeleton>
      ))}
    </div>
  );
}

/** 图表区域骨架（折线图/柱状图占位）。 */
export function ChartSkeleton({ className }: { className?: string }) {
  return (
    <CardSkeleton className={cn("p-5", className)}>
      <div className="mb-4 flex items-center justify-between">
        <Skeleton className="h-4 w-32" />
        <Skeleton className="h-4 w-24" />
      </div>
      <Skeleton className="h-48 w-full" />
    </CardSkeleton>
  );
}
