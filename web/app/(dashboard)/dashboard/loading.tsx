import {
  ChartSkeleton,
  PageHeaderSkeleton,
  StatCardsSkeleton,
  TableSkeleton,
} from "@/components/skeleton";

/** 概览页过渡：KPI 四宫格 + 明细表 + 趋势图，结构与 dashboard-live 对齐。 */
export default function Loading() {
  return (
    <div className="space-y-6">
      <PageHeaderSkeleton withActions={false} />
      <StatCardsSkeleton count={4} />
      <TableSkeleton rows={5} cols={6} />
      <ChartSkeleton />
    </div>
  );
}
