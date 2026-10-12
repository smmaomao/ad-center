"use server";

// 总览（Dashboard）Server Actions。
// 浏览器不直连 Go（BFF 转发，见 go-api.ts）：INTERNAL_API_KEY 只在服务端可见，
// 故客户端组件必须经此 action 由服务端注入密钥与操作者邮箱后再调用 Go。
import { getSession } from "@/lib/auth";
import {
  getCampaignCreativeStats,
  getDashboardDaily,
  getDashboardOverview,
  type CampaignCreativeStats,
  type DashboardDailyResult,
  type DashboardOverviewResult,
} from "@/lib/go-api";

export interface DashboardPayload {
  overview: DashboardOverviewResult;
  creatives: CampaignCreativeStats;
  daily: DashboardDailyResult;
}

/** 按日期区间加载总览 KPI + 素材维度分析（日期 YYYY-MM-DD，UTC 口径）；
 *  另附固定最近 7 天的「按日统计」趋势数据（不随上方日期区间变化）。 */
export async function loadDashboardAction(
  from: string,
  to: string,
): Promise<DashboardPayload> {
  const session = await getSession();
  if (!session) throw new Error("会话已过期，请重新登录");

  const [overview, creatives, daily] = await Promise.all([
    getDashboardOverview(session.email, { from, to }),
    getCampaignCreativeStats(session.email, { from, to }),
    getDashboardDaily(session.email, { days: 7 }),
  ]);
  return { overview, creatives, daily };
}
