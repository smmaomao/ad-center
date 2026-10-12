"use client";

import { useCallback, useEffect, useMemo, useState } from "react";

import { PageHeader } from "@/components/page-header";
import { Button } from "@/components/ui/button";
import { loadDashboardAction, type DashboardPayload } from "./actions";
import type { DashboardDailyRow } from "@/lib/go-api";

const WARNING_LABEL: Record<string, { text: string; cls: string }> = {
  paused: { text: "已暂停", cls: "bg-zinc-500/15 text-zinc-300" },
  budget: { text: "预算将尽", cls: "bg-amber-500/15 text-amber-300" },
  kpi: { text: "KPI偏离", cls: "bg-rose-500/15 text-rose-300" },
};

function fmtMoney(n: number): string {
  return "$" + (n ?? 0).toLocaleString("en-US", { maximumFractionDigits: 2 });
}
function fmtPct(n: number): string {
  if (!n) return "—";
  return (n * 100).toFixed(1) + "%";
}
function fmtRate(n: number): string {
  return ((n ?? 0) * 100).toFixed(2) + "%";
}
function fmtInt(n: number): string {
  return (n ?? 0).toLocaleString("en-US");
}
// 日期一律按 UTC 字符串处理，与后端 /v1/admin/dashboard/overview 的 UTC 口径一致。
function todayStr(): string {
  return new Date().toISOString().slice(0, 10);
}

export function DashboardLive() {
  const today = useMemo(() => todayStr(), []);

  const [from, setFrom] = useState(today);
  const [to, setTo] = useState(today);
  const [data, setData] = useState<DashboardPayload | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // 区间跨度（天），含首尾：0 = 同一天，6 = 跨 7 天
  const spanDays = useMemo(() => {
    const a = Date.parse(from + "T00:00:00Z");
    const b = Date.parse(to + "T00:00:00Z");
    if (Number.isNaN(a) || Number.isNaN(b)) return 0;
    return Math.round((b - a) / 86400000);
  }, [from, to]);
  const valid = spanDays >= 0; // 仅要求开始 <= 结束；区间不再限制 7 天

  // 经 server action 由服务端转发（浏览器不直连 Go，密钥只在服务端）
  const load = useCallback(async () => {
    if (!valid) {
      setError("开始日期不能晚于结束日期");
      return;
    }
    setLoading(true);
    setError(null);
    try {
      setData(await loadDashboardAction(from, to));
    } catch (e) {
      setError(e instanceof Error ? e.message : "加载失败");
    } finally {
      setLoading(false);
    }
  }, [from, to, valid]);

  // 仅首次挂载时加载（默认今日）；之后靠右上角「刷新」手动触发，不自动轮询。
  useEffect(() => {
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const ov = data?.overview.overview;
  const advertisers = data?.overview.advertisers ?? [];
  const creatives = data?.creatives.creatives ?? [];
  const totals = data?.creatives.totals;
  const dailyRows = data?.daily.data ?? [];

  return (
    <div className="space-y-6">
      <PageHeader
        title="总览"
        description="广告投放核心指标、广告主 KPI 与素材维度分析（按所选日期区间统计，UTC）"
        actions={
          <div className="flex items-end gap-2">
            <label className="flex flex-col gap-1 text-xs text-muted-foreground">
              开始日期
              <input
                type="date"
                value={from}
                max={today}
                onChange={(e) => setFrom(e.target.value)}
                className="h-9 rounded-md border bg-background px-2 text-sm text-foreground"
              />
            </label>
            <label className="flex flex-col gap-1 text-xs text-muted-foreground">
              结束日期
              <input
                type="date"
                value={to}
                max={today}
                onChange={(e) => setTo(e.target.value)}
                className="h-9 rounded-md border bg-background px-2 text-sm text-foreground"
              />
            </label>
            <Button onClick={() => void load()} disabled={loading || !valid}>
              {loading ? "刷新中…" : "刷新"}
            </Button>
          </div>
        }
      />

      {error && (
        <div className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {error}
        </div>
      )}

      {/* KPI 卡片 */}
      <div className="grid grid-cols-2 gap-4 md:grid-cols-4">
        <KpiCard
          label="今日消耗 / 日预算"
          value={ov ? `${fmtMoney(ov.spent_today)} / ${fmtMoney(ov.daily_budget)}` : "—"}
        />
        <KpiCard
          label="总体达成率"
          value={ov ? fmtPct(ov.achievement) : "—"}
          hint={ov ? "目标/实际 CPI 加权" : ""}
        />
        <KpiCard
          label="曝光 / 点击"
          value={ov ? `${fmtInt(ov.impressions)} / ${fmtInt(ov.clicks)}` : "—"}
        />
        <KpiCard label="安装" value={ov ? fmtInt(ov.install) : "—"} />
        <KpiCard label="激活" value={ov ? fmtInt(ov.activate) : "—"} />
        <KpiCard label="注册" value={ov ? fmtInt(ov.register) : "—"} />
        <KpiCard label="转化" value={ov ? fmtInt(ov.conversion) : "—"} hint="首充" />
        <KpiCard label="付费" value={ov ? fmtInt(ov.pay) : "—"} />
        <KpiCard label="填充率" value={ov ? fmtPct(ov.fill_rate) : "—"} />
        <KpiCard label="eCPM" value={ov ? fmtMoney(ov.ecpm) : "—"} />
        <KpiCard
          label="活跃广告主 / 任务"
          value={ov ? `${ov.active_advertisers} / ${ov.active_campaigns}` : "—"}
        />
      </div>

      {/* 广告主 KPI 监控 */}
      <section className="surface-card rise-1 p-5">
        <h2 className="mb-4 text-sm font-semibold text-foreground">广告主 KPI 监控</h2>
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead className="text-left text-muted-foreground">
              <tr className="border-b border-white/10">
                <th className="py-2 pr-4 font-medium">广告主</th>
                <th className="py-2 pr-4 font-medium">状态</th>
                <th className="py-2 pr-4 font-medium">日预算</th>
                <th className="py-2 pr-4 font-medium">今日消耗</th>
                <th className="py-2 pr-4 font-medium">目标CPI</th>
                <th className="py-2 pr-4 font-medium">达成率</th>
                <th className="py-2 font-medium">预警</th>
              </tr>
            </thead>
            <tbody>
              {advertisers.length === 0 && (
                <tr>
                  <td colSpan={7} className="py-6 text-center text-muted-foreground">
                    暂无数据
                  </td>
                </tr>
              )}
              {advertisers.map((a) => {
                const w = WARNING_LABEL[a.warning];
                return (
                  <tr key={a.id} className="border-b border-white/5">
                    <td className="py-2 pr-4 text-foreground">{a.name}</td>
                    <td className="py-2 pr-4 text-muted-foreground">{a.status}</td>
                    <td className="py-2 pr-4">{fmtMoney(a.daily_budget)}</td>
                    <td className="py-2 pr-4">{fmtMoney(a.spent_today)}</td>
                    <td className="py-2 pr-4">
                      {a.target_kpi_value ? fmtMoney(a.target_kpi_value) : "—"}
                    </td>
                    <td className="py-2 pr-4">{fmtPct(a.achievement)}</td>
                    <td className="py-2">
                      {w ? (
                        <span className={`rounded-full px-2 py-0.5 text-xs ${w.cls}`}>
                          {w.text}
                        </span>
                      ) : (
                        <span className="text-emerald-400">正常</span>
                      )}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </section>

      {/* 素材维度分析 + 按日统计（并排） */}
      <div className="grid gap-4 lg:grid-cols-3">
        <section className="surface-card rise-1 p-5 lg:col-span-2">
          <h2 className="mb-4 text-sm font-semibold text-foreground">素材维度分析</h2>
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead className="text-left text-muted-foreground">
                <tr className="border-b border-white/10">
                  <th className="py-2 pr-4 font-medium">素材</th>
                  <th className="py-2 pr-4 font-medium">素材 ID</th>
                  <th className="py-2 pr-4 font-medium">曝光</th>
                  <th className="py-2 pr-4 font-medium">点击</th>
                  <th className="py-2 font-medium">点击率</th>
                </tr>
              </thead>
              <tbody>
                {creatives.length === 0 && (
                  <tr>
                    <td colSpan={5} className="py-6 text-center text-muted-foreground">
                      暂无数据
                    </td>
                  </tr>
                )}
                {creatives.map((c) => (
                  <tr key={c.creative_id} className="border-b border-white/5">
                    <td className="py-2 pr-4 text-foreground">{c.name}</td>
                    <td className="py-2 pr-4 text-muted-foreground">{c.creative_id}</td>
                    <td className="py-2 pr-4">{fmtInt(c.impressions)}</td>
                    <td className="py-2 pr-4">{fmtInt(c.clicks)}</td>
                    <td className="py-2">{fmtRate(c.ctr)}</td>
                  </tr>
                ))}
                {totals && creatives.length > 0 && (
                  <tr className="font-medium">
                    <td className="py-2 pr-4 text-foreground">合计</td>
                    <td className="py-2 pr-4" />
                    <td className="py-2 pr-4">{fmtInt(totals.impressions)}</td>
                    <td className="py-2 pr-4">{fmtInt(totals.clicks)}</td>
                    <td className="py-2">{fmtRate(totals.ctr)}</td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        </section>

        <DailyStats rows={dailyRows} />
      </div>
    </div>
  );
}

// 按日统计：近 7 天全局曝光/点击/CTR 趋势（固定窗口，不随上方日期区间变化）。
// 纯 CSS 柱状条（高度 ∝ 当日曝光）+ 汇总行，不引图表库。
function DailyStats({ rows }: { rows: DashboardDailyRow[] }) {
  const max = Math.max(1, ...rows.map((r) => r.impressions));
  const sumImp = rows.reduce((a, r) => a + r.impressions, 0);
  const sumClk = rows.reduce((a, r) => a + r.clicks, 0);
  const avgCtr = sumImp > 0 ? sumClk / sumImp : 0;
  return (
    <section className="surface-card rise-1 p-5">
      <h2 className="mb-1 text-sm font-semibold text-foreground">按日统计</h2>
      <p className="mb-4 text-xs text-muted-foreground">
        近 {rows.length} 天 · 全局曝光/点击趋势（UTC）
      </p>
      <div className="flex h-36 items-end gap-1.5">
        {rows.map((r) => {
          const h = Math.round((r.impressions / max) * 100);
          const ctr = r.impressions > 0 ? r.clicks / r.impressions : 0;
          return (
            <div
              key={r.date}
              className="flex flex-1 flex-col items-center justify-end gap-1"
              title={`${r.date}\n曝光 ${fmtInt(r.impressions)}\n点击 ${fmtInt(r.clicks)}\nCTR ${fmtRate(ctr)}\n转化 ${fmtInt(r.conversions)}`}
            >
              <span className="text-[10px] leading-none text-muted-foreground">
                {fmtInt(r.impressions)}
              </span>
              <div
                className="w-full rounded-sm bg-brand"
                style={{ height: `${h}%`, minHeight: r.impressions > 0 ? 3 : 1 }}
              />
              <span className="text-[10px] leading-none text-muted-foreground">
                {r.date.slice(5)}
              </span>
              <span className="text-[10px] leading-none text-foreground/70">
                {fmtRate(ctr)}
              </span>
            </div>
          );
        })}
      </div>
      <div className="mt-4 grid grid-cols-3 gap-2 border-t border-white/10 pt-3 text-center">
        <div>
          <div className="text-[11px] text-muted-foreground">曝光合计</div>
          <div className="text-sm font-semibold">{fmtInt(sumImp)}</div>
        </div>
        <div>
          <div className="text-[11px] text-muted-foreground">点击合计</div>
          <div className="text-sm font-semibold">{fmtInt(sumClk)}</div>
        </div>
        <div>
          <div className="text-[11px] text-muted-foreground">平均CTR</div>
          <div className="text-sm font-semibold">{fmtRate(avgCtr)}</div>
        </div>
      </div>
    </section>
  );
}

function KpiCard({ label, value, hint }: { label: string; value: string; hint?: string }) {
  return (
    <div className="surface-card rise-1 p-4">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="mt-1 text-lg font-semibold text-foreground">{value}</div>
      {hint && <div className="mt-0.5 text-[11px] text-muted-foreground/70">{hint}</div>}
    </div>
  );
}
