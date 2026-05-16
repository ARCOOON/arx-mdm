import { useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { Power } from 'lucide-react'
import { useWebSocket } from '../hooks/useWebSocket'
import { formatBytesPair, formatCpu } from '../lib/format'

export function AssetsPage() {
  const { assets, sendJson, lastCommandResult } = useWebSocket()
  const [pending, setPending] = useState<string | null>(null)

  const hint = useMemo(() => {
    if (!lastCommandResult) {
      return null
    }
    const tone = lastCommandResult.ok ? 'text-emerald-400/90' : 'text-rose-400/90'
    return (
      <span className={`font-mono text-[11px] ${tone}`}>
        {lastCommandResult.message ?? (lastCommandResult.ok ? 'ok' : 'failed')}
      </span>
    )
  }, [lastCommandResult])

  return (
    <div className="min-h-full bg-slate-50 px-4 py-4 md:px-6 dark:bg-slate-950">
      <div className="mb-3 flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-lg font-semibold tracking-tight text-slate-900 dark:text-slate-100">
            Assets
          </h1>
          <p className="mt-0.5 text-xs text-slate-500">
            Live telemetry merges on each agent heartbeat. C2 shows command channel.
          </p>
        </div>
        {hint}
      </div>

      <div className="min-w-0 overflow-x-auto rounded border border-slate-200 bg-slate-100/80 dark:border-slate-800 dark:bg-slate-900/40">
        <table className="min-w-[720px] w-full border-collapse text-left text-[12px]">
          <thead>
            <tr className="border-b border-slate-200 bg-slate-100 dark:border-slate-800 dark:bg-slate-900/90 text-[11px] font-semibold uppercase tracking-wide text-slate-500">
              <th className="px-2 py-1.5">ARX ID</th>
              <th className="px-2 py-1.5">Hostname</th>
              <th className="px-2 py-1.5">OS</th>
              <th className="px-2 py-1.5">CPU</th>
              <th className="px-2 py-1.5">RAM (used / total)</th>
              <th className="px-2 py-1.5">C2</th>
              <th className="px-2 py-1.5 text-right">Actions</th>
            </tr>
          </thead>
          <tbody className="tabular-nums text-slate-800 dark:text-slate-200">
            {assets.length === 0 ? (
              <tr>
                <td
                  colSpan={7}
                  className="px-3 py-8 text-center text-xs text-slate-500"
                >
                  No assets in catalog. Connect an agent and post telemetry to
                  populate this view.
                </td>
              </tr>
            ) : (
              assets.map((a) => (
                <tr
                  key={a.human_id}
                  className="border-b border-slate-200/80 hover:bg-slate-100 dark:border-slate-800/80 dark:hover:bg-slate-800/30"
                >
                  <td className="px-2 py-1 font-mono text-[11px] text-sky-300/95">
                    <Link
                      to={`/assets/${encodeURIComponent(a.human_id)}`}
                      className="hover:underline"
                    >
                      {a.human_id}
                    </Link>
                  </td>
                  <td className="max-w-[200px] truncate px-2 py-1 font-medium text-slate-900 dark:text-slate-100">
                    {a.hostname || '—'}
                  </td>
                  <td className="max-w-[220px] truncate px-2 py-1 text-slate-400">
                    {a.os || '—'}
                  </td>
                  <td className="max-w-[280px] truncate px-2 py-1 text-slate-400">
                    {formatCpu(a.cpu_model, a.cpu_logical_cores, a.cpu_usage_percent)}
                  </td>
                  <td className="px-2 py-1 text-slate-300">
                    {formatBytesPair(a.memory_used_bytes, a.total_ram_bytes)}
                  </td>
                  <td className="px-2 py-1">
                    <span
                      className={
                        a.c2_connected
                          ? 'text-emerald-400'
                          : 'text-slate-600'
                      }
                    >
                      {a.c2_connected ? 'online' : 'offline'}
                    </span>
                  </td>
                  <td className="whitespace-nowrap px-2 py-1 text-right align-middle">
                    <button
                      type="button"
                      title="Send shutdown command"
                      disabled={pending === a.human_id}
                      className="inline-flex items-center gap-1 rounded border border-slate-300 dark:border-slate-700 bg-slate-900 px-2 py-0.5 text-[11px] font-medium text-slate-200 hover:border-rose-700/70 hover:bg-rose-950/40 hover:text-rose-100 disabled:cursor-not-allowed disabled:opacity-40"
                      onClick={() => {
                        setPending(a.human_id)
                        sendJson({
                          action: 'shutdown',
                          target_arx_id: a.human_id,
                        })
                        window.setTimeout(() => setPending(null), 1200)
                      }}
                    >
                      <Power className="size-3.5" />
                      Shutdown
                    </button>
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
    </div>
  )
}
