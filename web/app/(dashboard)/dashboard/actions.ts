"use server";

// 总览（Dashboard）Server Actions。
// 浏览器不直连 Go（BFF 转发，见 go-api.ts）：INTERNAL_API_KEY 只在服务端可见，
// 故客户端组件必须经此 action 由服务端注入密钥与操作者邮箱后再调用 Go。
import { getSession } from "@/lib/auth";
import {
  getCampaignCreativeStats,
  getDashboardCampaigns,
  getDashboardDaily,
  getDashboardOverview,
  type CampaignCreativeStats,
  type DashboardCampaignsResult,
  type DashboardDailyResult,
  type DashboardOverviewResult,
} from "@/lib/go-api";

export interface DashboardPayload {
  overview: DashboardOverviewResult;
  creatives: CampaignCreativeStats;
  daily: DashboardDailyResult;
  campaigns: DashboardCampaignsResult;
}

/** 按日期区间加载总览数据（日期 YYYY-MM-DD，UTC 口径）。
 *  - overview：今日快照类 KPI + 广告主 KPI 监控
 *  - campaigns：按广告任务维度拆分的曝光/点击/安装/激活/注册/转化/付费 + 按日趋势
 *  - daily / creatives：保留兼容（趋势与素材明细可另用） */
export async function loadDashboardAction(
  from: string,
  to: string,
): Promise<DashboardPayload> {
  const session = await getSession();
  if (!session) throw new Error("会话已过期，请重新登录");

  const [overview, creatives, daily, campaigns] = await Promise.all([
    getDashboardOverview(session.email, { from, to }),
    getCampaignCreativeStats(session.email, { from, to }),
    getDashboardDaily(session.email, { days: 7 }),
    getDashboardCampaigns(session.email, { from, to }),
  ]);
  return { overview, creatives, daily, campaigns };
}
