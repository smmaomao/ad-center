import { requireMenu } from "@/lib/auth";
import { DashboardLive } from "./dashboard-live";

export default async function DashboardPage() {
  // 页面级权限拦截；具体数据由 DashboardLive 经 server action 在服务端转发获取。
  await requireMenu("/dashboard");

  return <DashboardLive />;
}
