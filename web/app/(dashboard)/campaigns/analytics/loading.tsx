import { PageHeaderSkeleton, TableSkeleton } from "@/components/skeleton";

/** 数据分析页过渡：筛选栏 + 宽表（列较多）。 */
export default function Loading() {
  return (
    <div className="space-y-6">
      <PageHeaderSkeleton withActions={false} />
      <div className="flex flex-wrap items-center gap-3">
        <div className="h-9 w-40 animate-pulse rounded-md bg-muted" />
        <div className="h-9 w-32 animate-pulse rounded-md bg-muted" />
        <div className="h-9 w-28 animate-pulse rounded-md bg-muted" />
      </div>
      <TableSkeleton rows={8} cols={7} />
    </div>
  );
}
