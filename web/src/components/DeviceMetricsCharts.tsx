import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  CartesianGrid,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import { useTheme } from '../contexts/ThemeContext'
import {
  fetchDeviceMetrics,
  type DeviceMetricHistory,
} from '../lib/deviceMetricsApi'
import { formatBytesPair } from '../lib/format'
import type { ServerMessage } from '../types/ws'

type DeviceMetricsChartsProps = {
  deviceId: string
  humanId: string
  subscribeServerMessages: (handler: (msg: ServerMessage) => void) => () => void
}

function formatChartTime(iso: string): string {
  try {
    return new Date(iso).toLocaleTimeString(undefined, {
      hour: '2-digit',
      minute: '2-digit',
    })
  } catch {
    return iso
  }
}

export function DeviceMetricsCharts({
  deviceId,
  humanId,
  subscribeServerMessages,
}: DeviceMetricsChartsProps) {
  const { theme } = useTheme()
  const [hours, setHours] = useState(24)
  const [data, setData] = useState<DeviceMetricHistory | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const axisColor = theme === 'dark' ? '#94a3b8' : '#64748b'
  const gridColor = theme === 'dark' ? '#334155' : '#e2e8f0'
  const cpuStroke = '#38bdf8'
  const ramStroke = '#a78bfa'
  const diskFill = theme === 'dark' ? '#22c55e' : '#16a34a'
  const diskTrack = theme === 'dark' ? '#1e293b' : '#e2e8f0'

  const load = useCallback(async () => {
    if (!deviceId) {
      return
    }
    setLoading(true)
    setError(null)
    try {
      const h = await fetchDeviceMetrics(deviceId, hours)
      setData(h)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load metrics')
      setData(null)
    } finally {
      setLoading(false)
    }
  }, [deviceId, hours])

  useEffect(() => {
    void load()
  }, [load])

  useEffect(() => {
    return subscribeServerMessages((msg) => {
      if (msg.type !== 'telemetry_update') {
        return
      }
      const id = msg.asset.id
      const hid = msg.asset.human_id
      if (id === deviceId || hid === humanId) {
        void load()
      }
    })
  }, [subscribeServerMessages, deviceId, humanId, load])

  const chartRows = useMemo(() => {
    const series = data?.series ?? []
    return series.map((p) => ({
      ...p,
    }))
  }, [data])

  const diskPct =
    data?.disk && data.disk.total_bytes > 0
      ? Math.min(
          100,
          Math.round((data.disk.used_bytes / data.disk.total_bytes) * 100),
        )
      : null

  return (
    <div className="mb-6 space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h2 className="text-[11px] font-semibold uppercase tracking-wide text-slate-500">
          Telemetry history
        </h2>
        <div className="flex gap-1">
          {([6, 24, 72] as const).map((h) => (
            <button
              key={h}
              type="button"
              onClick={() => setHours(h)}
              className={`rounded px-2 py-0.5 text-[11px] font-medium ${
                hours === h
                  ? 'bg-slate-200 text-slate-900 ring-1 ring-slate-400 dark:bg-slate-800 dark:text-white dark:ring-slate-600'
                  : 'text-slate-600 hover:text-slate-800 dark:text-slate-500 dark:hover:text-slate-300'
              }`}
            >
              {h}h
            </button>
          ))}
        </div>
      </div>

      {error ? (
        <p className="text-[12px] text-rose-400">{error}</p>
      ) : null}

      {loading && !data ? (
        <p className="text-[12px] text-slate-500">Loading metrics…</p>
      ) : null}

      <div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3">
        {chartRows.length > 0 ? (
          <>
            <div className="rounded border border-slate-200 bg-slate-100/80 p-3 dark:border-slate-800 dark:bg-slate-900/40">
              <div className="mb-2 text-[10px] font-semibold uppercase text-slate-500">
                CPU usage (%)
              </div>
              <div className="h-[220px] w-full min-w-0">
                <ResponsiveContainer width="100%" height="100%">
                  <LineChart
                    data={chartRows}
                    margin={{ top: 4, right: 8, left: 0, bottom: 0 }}
                  >
                    <CartesianGrid stroke={gridColor} strokeDasharray="3 3" />
                    <XAxis
                      dataKey="t"
                      tick={{ fill: axisColor, fontSize: 10 }}
                      tickFormatter={formatChartTime}
                      minTickGap={24}
                    />
                    <YAxis
                      domain={[0, 100]}
                      tick={{ fill: axisColor, fontSize: 10 }}
                      width={32}
                    />
                    <Tooltip
                      contentStyle={{
                        backgroundColor:
                          theme === 'dark' ? '#0f172a' : '#f8fafc',
                        border: `1px solid ${gridColor}`,
                        borderRadius: 6,
                        fontSize: 12,
                      }}
                      labelFormatter={formatChartTime}
                      formatter={(value: number | string) => [
                        typeof value === 'number' ? `${value.toFixed(1)}%` : value,
                        'CPU',
                      ]}
                    />
                    <Line
                      type="monotone"
                      dataKey="cpu_usage"
                      stroke={cpuStroke}
                      strokeWidth={2}
                      dot={false}
                      isAnimationActive={false}
                    />
                  </LineChart>
                </ResponsiveContainer>
              </div>
            </div>

            <div className="rounded border border-slate-200 bg-slate-100/80 p-3 dark:border-slate-800 dark:bg-slate-900/40">
              <div className="mb-2 text-[10px] font-semibold uppercase text-slate-500">
                RAM used (%)
              </div>
              <div className="h-[220px] w-full min-w-0">
                <ResponsiveContainer width="100%" height="100%">
                  <LineChart
                    data={chartRows}
                    margin={{ top: 4, right: 8, left: 0, bottom: 0 }}
                  >
                    <CartesianGrid stroke={gridColor} strokeDasharray="3 3" />
                    <XAxis
                      dataKey="t"
                      tick={{ fill: axisColor, fontSize: 10 }}
                      tickFormatter={formatChartTime}
                      minTickGap={24}
                    />
                    <YAxis
                      domain={[0, 100]}
                      tick={{ fill: axisColor, fontSize: 10 }}
                      width={32}
                    />
                    <Tooltip
                      contentStyle={{
                        backgroundColor:
                          theme === 'dark' ? '#0f172a' : '#f8fafc',
                        border: `1px solid ${gridColor}`,
                        borderRadius: 6,
                        fontSize: 12,
                      }}
                      labelFormatter={formatChartTime}
                      formatter={(value: number | string) => [
                        typeof value === 'number' ? `${value.toFixed(1)}%` : value,
                        'RAM',
                      ]}
                    />
                    <Line
                      type="monotone"
                      dataKey="ram_used_percent"
                      stroke={ramStroke}
                      strokeWidth={2}
                      dot={false}
                      isAnimationActive={false}
                    />
                  </LineChart>
                </ResponsiveContainer>
              </div>
            </div>
          </>
        ) : !loading ? (
          <div className="rounded border border-dashed border-slate-300 bg-slate-100/50 p-4 text-[12px] text-slate-500 dark:border-slate-700 dark:bg-slate-900/30 md:col-span-2 lg:col-span-2">
            No metric history in this window. Samples appear after the agent sends
            telemetry.
          </div>
        ) : null}

        <div className="rounded border border-slate-200 bg-slate-100/80 p-3 dark:border-slate-800 dark:bg-slate-900/40 md:col-span-2 lg:col-span-1">
          <div className="mb-2 text-[10px] font-semibold uppercase text-slate-500">
            Root disk (latest sample)
          </div>
          {data?.disk && data.disk.total_bytes > 0 && diskPct !== null ? (
            <>
              <div className="mb-1 flex justify-between text-[11px] text-slate-600 dark:text-slate-400">
                <span>
                  {formatBytesPair(data.disk.used_bytes, data.disk.total_bytes)}
                </span>
                <span className="tabular-nums">{diskPct}%</span>
              </div>
              <div
                className="h-2.5 w-full overflow-hidden rounded-full"
                style={{ backgroundColor: diskTrack }}
                title={`Sampled at ${data.disk.sampled_at}`}
              >
                <div
                  className="h-full rounded-full transition-[width]"
                  style={{
                    width: `${diskPct}%`,
                    backgroundColor: diskFill,
                  }}
                />
              </div>
            </>
          ) : (
            <p className="text-[12px] text-slate-500">
              No disk samples in this window. Agents report disk totals over C2 or
              HTTP telemetry.
            </p>
          )}
        </div>
      </div>
    </div>
  )
}
