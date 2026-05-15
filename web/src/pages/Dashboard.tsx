import type { ReactNode } from 'react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Cell,
  Legend,
  Pie,
  PieChart,
  ResponsiveContainer,
  Tooltip,
} from 'recharts'
import {
  Activity,
  Cpu,
  HardDrive,
  Loader2,
  RefreshCw,
  Ticket,
  WifiOff,
} from 'lucide-react'
import { useWebSocket } from '../hooks/useWebSocket'
import {
  fetchAnalyticsSummary,
  type AnalyticsSummary,
} from '../lib/analyticsApi'

const PIE_COLORS = [
  '#38bdf8',
  '#34d399',
  '#fbbf24',
  '#a78bfa',
  '#fb7185',
  '#94a3b8',
]

function osChartData(dist: Record<string, number> | undefined) {
  if (!dist) return []
  const order = ['windows', 'linux', 'android', 'darwin', 'ios', 'unknown']
  const used = new Set<string>()
  const primary = order
    .filter((k) => (dist[k] ?? 0) > 0)
    .map((name) => {
      used.add(name)
      return { name, value: dist[name] ?? 0 }
    })
  const rest = Object.entries(dist)
    .filter(([k]) => !used.has(k))
    .map(([name, value]) => ({ name, value }))
  return [...primary, ...rest]
}

export function DashboardPage() {
  const { assets, connectionState, tickets } = useWebSocket()
  const [summary, setSummary] = useState<AnalyticsSummary | null>(null)
  const [summaryErr, setSummaryErr] = useState<string | null>(null)
  const [summaryLoading, setSummaryLoading] = useState(true)

  const loadSummary = useCallback(async () => {
    setSummaryErr(null)
    setSummaryLoading(true)
    try {
      const s = await fetchAnalyticsSummary()
      setSummary(s)
    } catch (e) {
      setSummaryErr(e instanceof Error ? e.message : 'analytics failed')
      setSummary(null)
    } finally {
      setSummaryLoading(false)
    }
  }, [])

  useEffect(() => {
    void loadSummary()
  }, [loadSummary])

  const c2Up = assets.filter((a) => a.c2_connected).length
  const avgCpu =
    assets.length === 0
      ? 0
      : assets.reduce((s, a) => s + (a.cpu_usage_percent || 0), 0) /
        assets.length

  const chartRows = useMemo(
    () => osChartData(summary?.os_distribution),
    [summary],
  )

  return (
    <div className="min-h-full border-b border-slate-800/80 bg-slate-950 px-6 py-4">
      <div className="mb-4 flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 className="text-lg font-semibold tracking-tight text-slate-100">
            Dashboard
          </h1>
          <p className="mt-0.5 text-xs text-slate-500">
            KPIs from REST analytics · fleet telemetry stream via{' '}
            <code className="rounded bg-slate-900 px-1 py-0.5 font-mono text-[11px] text-slate-400">
              /v1/ws
            </code>
          </p>
        </div>
        <div className="flex items-center gap-3 text-right text-[11px] text-slate-500">
          <button
            type="button"
            className="inline-flex items-center gap-1 rounded border border-slate-700 px-2 py-1 text-slate-300 hover:bg-slate-800"
            onClick={() => void loadSummary()}
          >
            <RefreshCw className="size-3.5" />
            Refresh analytics
          </button>
          <div>
            Stream{' '}
            <span className="font-mono text-slate-300">{connectionState}</span>
          </div>
        </div>
      </div>

      {summaryErr ? (
        <div className="mb-3 rounded border border-amber-900/50 bg-amber-950/30 px-3 py-2 font-mono text-[11px] text-amber-200/90">
          Analytics: {summaryErr}
        </div>
      ) : null}

      <div className="mb-3 grid grid-cols-2 gap-3 md:grid-cols-4">
        <MetricCard
          icon={<HardDrive className="size-4 text-sky-400" />}
          label="Total assets"
          value={
            summaryLoading && !summary ? (
              <InlineLoad />
            ) : (
              String(summary?.assets.total ?? '—')
            )
          }
        />
        <MetricCard
          icon={<Ticket className="size-4 text-violet-400" />}
          label="Open tickets"
          value={
            summaryLoading && !summary ? (
              <InlineLoad />
            ) : (
              String(summary?.tickets.unresolved ?? '—')
            )
          }
        />
        <MetricCard
          icon={<WifiOff className="size-4 text-rose-400" />}
          label="Offline devices"
          subtitle={
            summary
              ? `Online if last_seen within ${summary.online_threshold_seconds}s`
              : undefined
          }
          value={
            summaryLoading && !summary ? (
              <InlineLoad />
            ) : (
              String(summary?.assets.offline ?? '—')
            )
          }
        />
        <MetricCard
          icon={<Activity className="size-4 text-emerald-400" />}
          label="Online (telemetry)"
          value={
            summaryLoading && !summary ? (
              <InlineLoad />
            ) : (
              String(summary?.assets.online ?? '—')
            )
          }
        />
      </div>

      <div className="mb-3 grid grid-cols-2 gap-3 md:grid-cols-4">
        <MetricCard
          icon={<HardDrive className="size-4 text-slate-400" />}
          label="Live catalog (WS)"
          value={String(assets.length)}
        />
        <MetricCard
          icon={<Activity className="size-4 text-emerald-400" />}
          label="C2 sessions"
          value={`${c2Up} / ${assets.length}`}
        />
        <MetricCard
          icon={<Cpu className="size-4 text-amber-400" />}
          label="Mean CPU (sample)"
          value={`${avgCpu.toFixed(1)}%`}
        />
        <MetricCard
          icon={<Ticket className="size-4 text-slate-500" />}
          label="Tickets in stream"
          value={String(tickets.length)}
        />
      </div>

      <div className="grid gap-3 lg:grid-cols-2">
        <div className="rounded border border-slate-800 bg-slate-900/50 p-3">
          <h2 className="mb-2 text-[11px] font-semibold uppercase tracking-wide text-slate-500">
            OS distribution (registered)
          </h2>
          {summaryLoading && chartRows.length === 0 ? (
            <div className="flex h-[220px] items-center justify-center text-xs text-slate-500">
              <Loader2 className="mr-2 size-4 animate-spin" />
              Loading chart…
            </div>
          ) : chartRows.length === 0 ? (
            <p className="py-8 text-center text-xs text-slate-500">
              No assets in database yet.
            </p>
          ) : (
            <div className="h-[260px] w-full min-w-0">
              <ResponsiveContainer width="100%" height="100%">
                <PieChart>
                  <Pie
                    data={chartRows}
                    dataKey="value"
                    nameKey="name"
                    cx="50%"
                    cy="50%"
                    innerRadius={48}
                    outerRadius={88}
                    paddingAngle={2}
                  >
                    {chartRows.map((_, i) => (
                      <Cell
                        key={i}
                        fill={PIE_COLORS[i % PIE_COLORS.length]}
                        stroke="#0f172a"
                        strokeWidth={1}
                      />
                    ))}
                  </Pie>
                  <Tooltip
                    contentStyle={{
                      background: '#0f172a',
                      border: '1px solid #334155',
                      borderRadius: 6,
                      fontSize: 11,
                    }}
                  />
                  <Legend
                    wrapperStyle={{ fontSize: 11, color: '#94a3b8' }}
                    formatter={(value) => String(value)}
                  />
                </PieChart>
              </ResponsiveContainer>
            </div>
          )}
        </div>
        <div className="rounded border border-slate-800 bg-slate-900/50 p-3">
          <h2 className="mb-2 text-[11px] font-semibold uppercase tracking-wide text-slate-500">
            Raw OS counts
          </h2>
          <dl className="grid grid-cols-2 gap-x-3 gap-y-1 font-mono text-[11px] text-slate-300">
            {Object.entries(summary?.os_distribution ?? {}).map(([k, v]) => (
              <div key={k} className="contents">
                <dt className="text-slate-500">{k}</dt>
                <dd className="text-right tabular-nums">{v}</dd>
              </div>
            ))}
            {Object.keys(summary?.os_distribution ?? {}).length === 0 &&
            !summaryLoading ? (
              <div className="col-span-2 text-slate-500">No rows</div>
            ) : null}
          </dl>
        </div>
      </div>
    </div>
  )
}

function InlineLoad() {
  return (
    <span className="inline-flex items-center gap-1">
      <Loader2 className="size-4 animate-spin text-slate-500" />
    </span>
  )
}

function MetricCard(props: {
  icon: ReactNode
  label: string
  value: ReactNode
  subtitle?: string
}) {
  return (
    <div className="rounded border border-slate-800 bg-slate-900/60 px-3 py-2.5 shadow-sm">
      <div className="mb-1 flex items-center gap-2 text-[11px] font-medium uppercase tracking-wide text-slate-500">
        {props.icon}
        {props.label}
      </div>
      <div className="font-mono text-xl font-semibold tabular-nums text-slate-100">
        {props.value}
      </div>
      {props.subtitle ? (
        <div className="mt-0.5 text-[10px] text-slate-600">{props.subtitle}</div>
      ) : null}
    </div>
  )
}
