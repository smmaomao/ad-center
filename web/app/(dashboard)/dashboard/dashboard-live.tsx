"use client";

import {
  Fragment,
  useCallback,
  useEffect,
  useMemo,
  useState,
  type Dispatch,
  type SetStateAction,
} from "react";

import { PageHeader } from "@/components/page-header";
import { Button } from "@/components/ui/button";
import { loadDashboardAction, type DashboardPayload } from "./actions";
import type {
  DashboardCampaignDaily,
  DashboardCreativeDaily,
  DashboardDay,
} from "@/lib/go-api";

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
  // 默认展示最近 7 天（与「按日趋势」语义一致），日期框仍可放宽到任意区间
  const initialFrom = useMemo(() => {
    const d = new Date(today + "T00:00:00Z");
    d.setUTCDate(d.getUTCDate() - 6);
    return d.toISOString().slice(0, 10);
  }, [today]);

  const [from, setFrom] = useState(initialFrom);
  const [to, setTo] = useState(today);
  const [data, setData] = useState<DashboardPayload | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // 按日分组表格的展开状态：openDays=展开的日期；openCamps=展开的「日期::任务」明细
  const [openDays, setOpenDays] = useState<Set<string>>(new Set());
  const [openCamps, setOpenCamps] = useState<Set<string>>(new Set());
  const toggleSet = (
    setter: Dispatch<SetStateAction<Set<string>>>,
    key: string,
  ) =>
    setter((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });

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
  const campaigns = data?.campaigns.campaigns ?? [];
  const days = data?.campaigns.days ?? [];
  // 按日期倒序展示（最新一天在最上面）
  const sortedDays = useMemo(
    () => [...days].sort((a, b) => b.date.localeCompare(a.date)),
    [days],
  );

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

      {/* KPI 卡片（仅快照类指标；区间指标改按广告任务维度展示） */}
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
              <tr className="border-b border-border">
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
                  <tr key={a.id} className="border-b border-border/60 transition-colors hover:bg-muted/30">
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

      {/* 广告任务维度分析（按日分组，每天一行，可展开到素材明细） */}
      <section className="surface-card rise-2 p-5 sm:p-6">
        <div className="mb-5">
          <h2 className="text-[15px] font-semibold tracking-tight text-foreground">
            广告任务维度分析
            <span className="ml-2 rounded-full bg-brand/10 px-2 py-0.5 align-middle text-[10px] font-semibold uppercase tracking-wider text-brand">
              按日
            </span>
          </h2>
          <p className="mt-1.5 text-xs leading-relaxed text-muted-foreground">
            按各任务统计时区切分本地日 · 区间{" "}
            <span className="font-mono text-foreground/80">{from}</span>
            {" ~ "}
            <span className="font-mono text-foreground/80">{to}</span>
            {" · "}日期倒序（最新在上）· 含已停止但窗口内有活动的任务
          </p>
        </div>

        <div className="space-y-2.5">
          {sortedDays.length === 0 && (
            <div className="flex flex-col items-center gap-1.5 rounded-xl border border-dashed py-12 text-sm text-muted-foreground">
              <span className="font-mono text-lg text-muted-foreground/40">∅</span>
              所选区间暂无数据
            </div>
          )}
          {sortedDays.map((day, di) => {
            const dayOpen = openDays.has(day.date);
            const dayImp = day.campaigns.reduce((a, c) => a + c.impressions, 0);
            const dayClk = day.campaigns.reduce((a, c) => a + c.clicks, 0);
            const dayCtr = dayImp > 0 ? dayClk / dayImp : 0;
            const dayInst = day.campaigns.reduce((a, c) => a + c.install, 0);
            const dayAct = day.campaigns.reduce((a, c) => a + c.activate, 0);
            const dayReg = day.campaigns.reduce((a, c) => a + c.register, 0);
            const dayConv = day.campaigns.reduce((a, c) => a + c.conversion, 0);
            const dayPay = day.campaigns.reduce((a, c) => a + c.pay, 0);
            return (
              <div
                key={day.date}
                className={`overflow-hidden rounded-xl border transition-colors duration-200 ${
                  dayOpen ? "border-brand/35 bg-brand/[0.03]" : "border-border/80 hover:border-border"
                }`}
                style={{ animationDelay: `${Math.min(di, 8) * 40}ms` }}
              >
                <button
                  type="button"
                  onClick={() => toggleSet(setOpenDays, day.date)}
                  className="flex w-full items-center gap-3 px-3.5 py-3 text-left transition-colors hover:bg-muted/40 sm:gap-4 sm:px-4"
                >
                  <span
                    className={`flex size-7 shrink-0 items-center justify-center rounded-lg border transition-all duration-200 ${
                      dayOpen
                        ? "rotate-90 border-brand/40 bg-brand/15 text-brand"
                        : "border-border text-muted-foreground"
                    }`}
                  >
                    <ChevronIcon className="size-3.5" />
                  </span>
                  <span className="min-w-0">
                    <span className="flex items-baseline gap-2">
                      <span className="font-mono text-[15px] font-semibold tracking-tight text-foreground">
                        {day.date}
                      </span>
                      <span className="rounded bg-muted px-1.5 py-px text-[10px] font-medium text-muted-foreground">
                        {weekdayCn(day.date)}
                      </span>
                    </span>
                    <span className="mt-0.5 block text-[11px] text-muted-foreground">
                      {day.campaigns.length} 个广告任务
                    </span>
                  </span>
                  <span className="ml-auto flex shrink-0 items-center gap-3.5 sm:gap-6">
                    <MiniStat label="曝光" value={fmtInt(dayImp)} />
                    <MiniStat label="点击" value={fmtInt(dayClk)} />
                    <span className="hidden items-center gap-3.5 sm:gap-6 lg:flex">
                      <MiniStat label="安装" value={fmtCnt(dayInst)} />
                      <MiniStat label="激活" value={fmtCnt(dayAct)} />
                      <MiniStat label="注册" value={fmtCnt(dayReg)} />
                      <MiniStat label="转化" value={fmtCnt(dayConv)} />
                      <MiniStat label="付费" value={fmtCnt(dayPay)} />
                    </span>
                    <span className="hidden sm:block">
                      <MiniStat label="CTR" value={fmtRate(dayCtr)} accent />
                    </span>
                  </span>
                </button>

                {dayOpen && (
                  <div className="animate-in fade-in slide-in-from-top-1 overflow-x-auto border-t border-border/70 duration-200">
                    <table className="w-full text-sm">
                      <thead>
                        <tr className="text-left text-[11px] uppercase tracking-wider text-muted-foreground/80">
                          <th className="py-2 pl-4 pr-3 font-medium sm:pl-[3.75rem]">广告任务</th>
                          <th className="px-3 py-2 font-medium">状态</th>
                          <th className="px-3 py-2 font-medium">时区</th>
                          <th className="px-3 py-2 text-right font-medium">曝光</th>
                          <th className="px-3 py-2 text-right font-medium">点击</th>
                          <th className="px-3 py-2 text-right font-medium">安装</th>
                          <th className="px-3 py-2 text-right font-medium">激活</th>
                          <th className="px-3 py-2 text-right font-medium">注册</th>
                          <th className="px-3 py-2 text-right font-medium">转化</th>
                          <th className="py-2 pl-3 pr-4 text-right font-medium">付费</th>
                        </tr>
                      </thead>
                      <tbody>
                        {day.campaigns.map((c) => {
                          const key = `${day.date}::${c.campaign_id}`;
                          const cOpen = openCamps.has(key);
                          return (
                            <Fragment key={c.campaign_id}>
                              <tr className="border-t border-border/50 transition-colors hover:bg-muted/30">
                                <td className="py-2.5 pl-4 pr-3 sm:pl-[3.75rem]">
                                  <button
                                    type="button"
                                    onClick={() => toggleSet(setOpenCamps, key)}
                                    className="group/c flex items-center gap-1.5 text-left"
                                  >
                                    <ChevronIcon
                                      className={`size-3 shrink-0 text-muted-foreground/60 transition-transform duration-200 ${
                                        cOpen ? "rotate-90" : ""
                                      }`}
                                    />
                                    <span className="font-medium text-foreground group-hover/c:text-brand">
                                      {c.name}
                                    </span>
                                  </button>
                                </td>
                                <td className="px-3 py-2.5">
                                  <StatusDot status={c.status} />
                                </td>
                                <td className="px-3 py-2.5 font-mono text-xs text-muted-foreground">
                                  {c.timezone}
                                </td>
                                <td className="px-3 py-2.5 text-right"><Num v={c.impressions} /></td>
                                <td className="px-3 py-2.5 text-right"><Num v={c.clicks} /></td>
                                <td className="px-3 py-2.5 text-right"><Num v={c.install} /></td>
                                <td className="px-3 py-2.5 text-right"><Num v={c.activate} /></td>
                                <td className="px-3 py-2.5 text-right"><Num v={c.register} /></td>
                                <td className="px-3 py-2.5 text-right"><Num v={c.conversion} /></td>
                                <td className="py-2.5 pl-3 pr-4 text-right"><Num v={c.pay} /></td>
                              </tr>
                              {cOpen && (
                                <tr>
                                  <td colSpan={10} className="bg-muted/20 p-0">
                                    <div className="animate-in fade-in slide-in-from-top-1 px-4 pb-4 pt-1 duration-200 sm:pl-[3.75rem]">
                                      <CreativeDetail creatives={c.creatives} />
                                    </div>
                                  </td>
                                </tr>
                              )}
                            </Fragment>
                          );
                        })}
                      </tbody>
                    </table>
                  </div>
                )}
              </div>
            );
          })}
        </div>
      </section>

      {/* 按日趋势：按广告任务细分（每任务一张双折线图） */}
      <section className="surface-card rise-1 p-5">
        <h2 className="mb-1 text-sm font-semibold text-foreground">按日趋势（按广告任务细分）</h2>
        <p className="mb-4 text-xs text-muted-foreground">
          近区间每日 曝光/点击（各任务按自身时区切分，双轴各自归一，悬停查看数值）。含已停止任务。
        </p>
        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
          {campaigns.length === 0 && (
            <div className="py-6 text-center text-muted-foreground">暂无数据</div>
          )}
          {campaigns.map((c) => (
            <div key={c.campaign_id} className="rounded-lg border border-border p-3 transition-colors hover:border-brand/30">
              <div className="mb-1 flex items-center justify-between gap-2">
                <span className="truncate text-sm font-medium text-foreground">{c.name}</span>
                <span className="ml-2 shrink-0 text-[10px] text-muted-foreground">{c.timezone}</span>
              </div>
              <div className="mb-1">
                {c.status === "active" ? (
                  <span className="rounded-full bg-emerald-500/15 px-2 py-0.5 text-[10px] text-emerald-300">
                    投放中
                  </span>
                ) : (
                  <span className="rounded-full bg-zinc-500/15 px-2 py-0.5 text-[10px] text-zinc-300">
                    已停止
                  </span>
                )}
              </div>
              <TrendChart daily={c.daily} />
            </div>
          ))}
        </div>
      </section>
    </div>
  );
}

// 按日趋势双折线图：曝光（品牌色）+ 点击（蓝），各自按自身最大值归一，
// 带横坐标与悬停数值气泡。用于「按广告任务细分」的每任务一张小图。
function TrendChart({ daily }: { daily: DashboardCampaignDaily[] }) {
  const [hover, setHover] = useState<number | null>(null);
  const sumImp = daily.reduce((a, r) => a + r.impressions, 0);
  const sumClk = daily.reduce((a, r) => a + r.clicks, 0);
  const avgCtr = sumImp > 0 ? sumClk / sumImp : 0;

  const W = 300, H = 120, padL = 8, padR = 8, padT = 12, padB = 20;
  const innerW = W - padL - padR;
  const innerH = H - padT - padB;
  const axisY = padT + innerH;
  const n = daily.length;
  const maxImp = Math.max(1, ...daily.map((r) => r.impressions));
  const maxClk = Math.max(1, ...daily.map((r) => r.clicks));

  const xAt = (i: number) => padL + (n <= 1 ? innerW / 2 : (i * innerW) / (n - 1));
  const yImp = (v: number) => padT + (1 - v / maxImp) * innerH;
  const yClk = (v: number) => padT + (1 - v / maxClk) * innerH;

  const impPts = daily.map((r, i) => `${xAt(i).toFixed(1)},${yImp(r.impressions).toFixed(1)}`).join(" ");
  const clkPts = daily.map((r, i) => `${xAt(i).toFixed(1)},${yClk(r.clicks).toFixed(1)}`).join(" ");

  // 悬停气泡定位（避免越出右/左边界）
  const tipW = 96, tipH = 54;
  const hx = hover != null ? xAt(hover) : 0;
  let tipX = hx - tipW / 2;
  if (tipX < 2) tipX = 2;
  if (tipX + tipW > W - 2) tipX = W - 2 - tipW;

  return (
    <div>
      <svg
        viewBox={`0 0 ${W} ${H}`}
        className="w-full"
        preserveAspectRatio="xMidYMid meet"
        onMouseLeave={() => setHover(null)}
      >
        {/* 横坐标 */}
        <line x1={padL} y1={axisY} x2={W - padR} y2={axisY} stroke="var(--border)" strokeWidth={1} />

        {/* 悬停竖向辅助线 */}
        {hover != null && (
          <line
            x1={hx}
            y1={padT}
            x2={hx}
            y2={axisY}
            stroke="var(--muted-foreground)"
            strokeWidth={1}
            strokeDasharray="3 3"
          />
        )}

        <polyline
          points={impPts}
          fill="none"
          stroke="var(--brand)"
          strokeWidth={2}
          strokeLinejoin="round"
          strokeLinecap="round"
        />
        <polyline
          points={clkPts}
          fill="none"
          stroke="var(--chart-2)"
          strokeWidth={2}
          strokeLinejoin="round"
          strokeLinecap="round"
        />

        {daily.map((r, i) => (
          <g key={r.date}>
            <circle
              cx={xAt(i)}
              cy={yImp(r.impressions)}
              r={hover === i ? 3.4 : 2.4}
              fill="var(--brand)"
              stroke="var(--card)"
              strokeWidth={1}
            />
            <circle
              cx={xAt(i)}
              cy={yClk(r.clicks)}
              r={hover === i ? 3.4 : 2.4}
              fill="var(--chart-2)"
              stroke="var(--card)"
              strokeWidth={1}
            />
          </g>
        ))}

        {daily.map((r, i) => (
          <text
            key={r.date}
            x={xAt(i)}
            y={H - 5}
            textAnchor="middle"
            className="fill-muted-foreground"
            style={{ fontSize: 9 }}
          >
            {r.date.slice(5)}
          </text>
        ))}

        {/* 透明热区：整列可触发悬停 */}
        {daily.map((r, i) => (
          <rect
            key={`hit-${r.date}`}
            x={Math.max(0, xAt(i) - innerW / (2 * n))}
            y={padT}
            width={innerW / n}
            height={innerH}
            fill="transparent"
            style={{ cursor: "pointer" }}
            onMouseEnter={() => setHover(i)}
          />
        ))}

        {/* 悬停数值气泡 */}
        {hover != null &&
          (() => {
            const r = daily[hover];
            const ctr = r.impressions > 0 ? r.clicks / r.impressions : 0;
            return (
              <g style={{ pointerEvents: "none" }}>
                <rect
                  x={tipX}
                  y={padT + 2}
                  width={tipW}
                  height={tipH}
                  rx={4}
                  fill="var(--ink)"
                  stroke="var(--border)"
                  strokeWidth={0.5}
                />
                <text x={tipX + 7} y={padT + 14} className="fill-foreground" style={{ fontSize: 9, fontWeight: 600 }}>
                  {r.date}
                </text>
                <circle cx={tipX + 10} cy={padT + 27} r={2.4} fill="var(--brand)" />
                <text x={tipX + 16} y={padT + 30} className="fill-muted-foreground" style={{ fontSize: 8.5 }}>
                  曝光 {fmtInt(r.impressions)}
                </text>
                <circle cx={tipX + 10} cy={padT + 40} r={2.4} fill="var(--chart-2)" />
                <text x={tipX + 16} y={padT + 43} className="fill-muted-foreground" style={{ fontSize: 8.5 }}>
                  点击 {fmtInt(r.clicks)}
                </text>
                <text x={tipX + 16} y={padT + 51} className="fill-muted-foreground" style={{ fontSize: 8.5 }}>
                  CTR {fmtRate(ctr)}
                </text>
              </g>
            );
          })()}
      </svg>
      <div className="mt-2 flex items-center justify-center gap-3 text-[11px] text-muted-foreground">
        <span className="flex items-center gap-1"><span className="inline-block h-2 w-2 rounded-full bg-brand" />曝光 {fmtInt(sumImp)}</span>
        <span className="flex items-center gap-1"><span className="inline-block h-2 w-2 rounded-full" style={{ background: "var(--chart-2)" }} />点击 {fmtInt(sumClk)}</span>
        <span>CTR {fmtRate(avgCtr)}</span>
      </div>
    </div>
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

/* ---- 按日分析区块的子组件 ---- */

const WEEKDAYS_CN = ["周日", "周一", "周二", "周三", "周四", "周五", "周六"];
function weekdayCn(date: string): string {
  const d = new Date(date + "T00:00:00Z");
  return Number.isNaN(d.getTime()) ? "" : WEEKDAYS_CN[d.getUTCDay()];
}

function ChevronIcon({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 16 16" fill="none" className={className} aria-hidden="true">
      <path
        d="M6 4l4 4-4 4"
        stroke="currentColor"
        strokeWidth="1.8"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}

// 数字单元格：0 显示为淡横杠，减少满屏 0 的视觉噪音
function Num({ v }: { v: number }) {
  if (!v) return <span className="text-muted-foreground/35">—</span>;
  return <span className="font-mono tabular-nums text-foreground">{fmtInt(v)}</span>;
}

// 汇总指标：0 显示为淡横杠
function fmtCnt(n: number): string {
  return n > 0 ? fmtInt(n) : "—";
}

// 日头部右侧的迷你指标
function MiniStat({ label, value, accent }: { label: string; value: string; accent?: boolean }) {
  return (
    <span className="text-right">
      <span className="block text-[10px] font-medium uppercase tracking-wider text-muted-foreground/70">
        {label}
      </span>
      <span
        className={`block font-mono text-sm font-semibold tabular-nums ${
          accent ? "text-brand" : "text-foreground"
        }`}
      >
        {value}
      </span>
    </span>
  );
}

// 状态：色点 + 文案（替代亮色下发灰的胶囊徽章）
function StatusDot({ status }: { status: string }) {
  const active = status === "active";
  return (
    <span className="inline-flex items-center gap-1.5 whitespace-nowrap">
      <span
        className={`size-1.5 rounded-full ${
          active ? "bg-emerald-500 shadow-[0_0_6px] shadow-emerald-500/40" : "bg-muted-foreground/40"
        }`}
      />
      <span className={`text-xs ${active ? "text-foreground" : "text-muted-foreground"}`}>
        {active ? "投放中" : "已停止"}
      </span>
    </span>
  );
}

// CTR：数值 + 相对当日最高 CTR 的微型条形图
function CtrBar({ ctr, max }: { ctr: number; max: number }) {
  const pct = Math.max(2, Math.min(100, (ctr / max) * 100));
  return (
    <span className="inline-flex items-center justify-end gap-2">
      <span className="font-mono tabular-nums">{fmtRate(ctr)}</span>
      <span className="hidden h-1.5 w-16 overflow-hidden rounded-full bg-muted sm:block">
        <span
          className="block h-full rounded-full bg-brand/80"
          style={{ width: `${ctr > 0 ? pct : 0}%` }}
        />
      </span>
    </span>
  );
}

// 关联素材明细面板
function CreativeDetail({ creatives }: { creatives: DashboardCreativeDaily[] }) {
  const maxCtr = Math.max(1e-4, ...creatives.map((cr) => cr.ctr));
  return (
    <div className="rounded-xl border border-border/60 bg-card p-4 shadow-xs">
      <div className="mb-3 flex items-center gap-2 text-[11px] font-medium uppercase tracking-wider text-muted-foreground">
        <span className="h-3 w-0.5 rounded-full bg-brand" />
        关联素材明细
        <span className="rounded bg-muted px-1.5 py-px font-mono text-[10px] normal-case tracking-normal">
          {creatives.length}
        </span>
      </div>
      {creatives.length === 0 ? (
        <p className="py-1 text-xs text-muted-foreground">当日无素材曝光/点击（仅有转化回传）</p>
      ) : (
        <table className="w-full text-sm">
          <thead>
            <tr className="text-left text-[11px] uppercase tracking-wider text-muted-foreground/80">
              <th className="pb-2 pr-4 font-medium">素材</th>
              <th className="px-3 pb-2 text-right font-medium">曝光</th>
              <th className="px-3 pb-2 text-right font-medium">点击</th>
              <th className="pb-2 pl-3 text-right font-medium">CTR</th>
            </tr>
          </thead>
          <tbody>
            {creatives.map((cr) => (
              <tr key={cr.creative_id} className="border-t border-border/50">
                <td className="py-2 pr-4">
                  <span className="line-clamp-1 text-foreground">{cr.name || cr.creative_id}</span>
                </td>
                <td className="px-3 py-2 text-right"><Num v={cr.impressions} /></td>
                <td className="px-3 py-2 text-right"><Num v={cr.clicks} /></td>
                <td className="py-2 pl-3 text-right">
                  <CtrBar ctr={cr.ctr} max={maxCtr} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}
