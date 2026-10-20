import { PageHeaderSkeleton, TableSkeleton } from "@/components/skeleton";

/**
 * (dashboard) 级通用导航过渡：覆盖广告主 / 任务 / 素材 / 应用 / 商品 /
 * 设置 等所有列表页与详情页。
 *
 * 菜单切换时 page.tsx 需在服务端串行取身份+菜单、再取列表数据（两趟 Go
 * 请求），期间没有 loading 边界的话旧页面会一直不动、观感就是"卡"。
 * 这里立刻渲染占位结构，让用户马上看到反馈。
 */
export default function DashboardLoading() {
  return (
    <div className="space-y-6">
      <PageHeaderSkeleton />
      <TableSkeleton />
    </div>
  );
}
