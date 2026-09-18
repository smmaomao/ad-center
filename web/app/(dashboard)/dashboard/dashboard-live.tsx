"use client";

import { useEffect, useRef, useState } from "react";

interface Overview {
  daily_budget: number;
  spent_today: number;
  achievement: number;
  requests: number;
  fills: number;
  impressions: number;
  clicks: number;
  conversions: number;
  revenue: number;
  fill_rate: number;
  ecpm: number;
  active_advertisers: number;
  active_campaigns: number;
}

interface Advertiser {
  id: string;
  name: string;
  status: string;
  daily_budget: number;
  spent_today: number;
  target_kpi_type: string;
  target_kpi_value: number;
  achievement: number;
  warning: string;
}

interface Slot {
  code: string;
  slot_key: string;
  name: string;
  type: string;
  status: string;
  app_name: string;
  fill_count: number;
  requests_today: number;
  fills_today: number;
}

interface Queue {
  buffered: number;
  pending: number;
  dropped: number;
}

interface Snapshot {
  overview: Overview | null;
  advertisers: Advertiser[];
  slots: Slot[];
  queue: Queue;
  server_time: number;
}

const EMPTY: Snapshot = {
  overview: null,
  advertisers: [],
  slots: [],
  queue: { buffered: 0, pending: 0, dropped: 0 },
  server_time: 0,
};

function fmtMoney(n: number): string {
  return "$" + (n ?? 0).toLocaleString("en-US", { maximumFractionDigits: 2 });
}
function fmtPct(n: number): string {
  if (!n) return "—";
  return (n * 100).toFixed(1) + "%";
}
function fmtInt(n: number): string {
  return (n ?? 0).toLocaleString("en-US");
}

const WARNING_LABEL: Record<string, { text: string; cls: string }> = {
  paused: { text: "已暂停", cls: "bg-zinc-500/15 text-zinc-300" },
  budget: { text: "预算将尽", cls: "bg-amber-500/15 text-amber-300" },
  kpi: { text: "KPI偏离", cls: "bg-rose-500/15 text-rose-300" },
};

export function DashboardLive() {
  const [snap, setSnap] = useState<Snapshot>(EMPTY);
  const [connected, setConnected] = useState(false);
  const esRef = useRef<EventSource | null>(null);

  useEffect(() => {
    const es = new EventSource("/api/metrics/stream");
    esRef.current = es;
    es.onopen = () => setConnected(true);
    es.onmessage = (e) => {
      try {
        setSnap(JSON.parse(e.data) as Snapshot);
        setConnected(true);
      } catch {
        /* 忽略非法帧 */
      }
    };
    es.onerror = () => setConnected(false);
    return () => es.close();
  }, []);

  const ov = snap.overview;

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-2 text-sm">
        <span
          className={`inline-block h-2 w-2 rounded-full ${connected ? "bg-emerald-400" : "bg-zinc-500"}`}
        />
        <span className="text-muted-foreground">
          {connected ? "实时连接中（每 10 秒刷新）" : "连接中断，正在重连…"}
        </span>
      </div>

      {/* KPI 卡片 */}
      <div className="grid grid-cols-2 gap-4 md:grid-cols-4">
        <KpiCard label="今日消耗 / 日预算" value={ov ? `${fmtMoney(ov.spent_today)} / ${fmtMoney(ov.daily_budget)}` : "—"} />
        <KpiCard label="总体达成率" value={ov ? fmtPct(ov.achievement) : "—"} hint={ov ? `目标/实际 CPI 加权` : ""} />
        <KpiCard label="曝光 / 点击" value={ov ? `${fmtInt(ov.impressions)} / ${fmtInt(ov.clicks)}` : "—"} />
        <KpiCard label="转化" value={ov ? fmtInt(ov.conversions) : "—"} />
        <KpiCard label="填充率" value={ov ? fmtPct(ov.fill_rate) : "—"} />
        <KpiCard label="eCPM" value={ov ? fmtMoney(ov.ecpm) : "—"} />
        <KpiCard label="活跃广告主 / 任务" value={ov ? `${ov.active_advertisers} / ${ov.active_campaigns}` : "—"} />
        <KpiCard
          label="队列积压"
          value={`${fmtInt(snap.queue.buffered)} / ${fmtInt(snap.queue.pending)}`}
          hint={`丢弃 ${fmtInt(snap.queue.dropped)}`}
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
              {snap.advertisers.length === 0 && (
                <tr>
                  <td colSpan={8} className="py-6 text-center text-muted-foreground">
                    暂无数据
                  </td>
                </tr>
              )}
              {snap.advertisers.map((a) => {
                const w = WARNING_LABEL[a.warning];
                return (
                  <tr key={a.id} className="border-b border-white/5">
                    <td className="py-2 pr-4 text-foreground">{a.name}</td>
                    <td className="py-2 pr-4 text-muted-foreground">{a.status}</td>
                    <td className="py-2 pr-4">{fmtMoney(a.daily_budget)}</td>
                    <td className="py-2 pr-4">{fmtMoney(a.spent_today)}</td>
                    <td className="py-2 pr-4">{a.target_kpi_value ? fmtMoney(a.target_kpi_value) : "—"}</td>
                    <td className="py-2 pr-4">{fmtPct(a.achievement)}</td>
                    <td className="py-2">
                      {w ? (
                        <span className={`rounded-full px-2 py-0.5 text-xs ${w.cls}`}>{w.text}</span>
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

      {/* 广告位实时状态 */}
      <section className="surface-card rise-1 p-5">
        <h2 className="mb-4 text-sm font-semibold text-foreground">广告位实时状态</h2>
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead className="text-left text-muted-foreground">
              <tr className="border-b border-white/10">
                <th className="py-2 pr-4 font-medium">广告位</th>
                <th className="py-2 pr-4 font-medium">应用</th>
                <th className="py-2 pr-4 font-medium">类型</th>
                <th className="py-2 pr-4 font-medium">状态</th>
                <th className="py-2 pr-4 font-medium">优先级条数</th>
                <th className="py-2 pr-4 font-medium">今日请求</th>
                <th className="py-2 font-medium">今日填充</th>
              </tr>
            </thead>
            <tbody>
              {snap.slots.length === 0 && (
                <tr>
                  <td colSpan={7} className="py-6 text-center text-muted-foreground">
                    暂无数据
                  </td>
                </tr>
              )}
              {snap.slots.map((s) => (
                <tr key={s.code} className="border-b border-white/5">
                  <td className="py-2 pr-4 text-foreground">{s.name}</td>
                  <td className="py-2 pr-4 text-muted-foreground">{s.app_name}</td>
                  <td className="py-2 pr-4 text-muted-foreground">{s.type}</td>
                  <td className="py-2 pr-4">
                    <span className={s.status === "active" ? "text-emerald-400" : "text-zinc-400"}>
                      {s.status}
                    </span>
                  </td>
                  <td className="py-2 pr-4">{s.fill_count}</td>
                  <td className="py-2 pr-4">{fmtInt(s.requests_today)}</td>
                  <td className="py-2">{fmtInt(s.fills_today)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>
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
