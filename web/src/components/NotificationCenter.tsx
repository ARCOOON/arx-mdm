import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Bell, CheckCircle2 } from 'lucide-react'
import { useAuth } from '../context/AuthContext'
import { shell } from '../lib/themeClasses'
import {
  type ActiveAlertWire,
  fetchActiveAlerts,
  resolveActiveAlert,
} from '../lib/alertsApi'

function severityChipClass(severity: string) {
  switch (severity) {
    case 'critical':
      return 'border-rose-300 bg-rose-50 text-rose-800 dark:border-rose-900/70 dark:bg-rose-950/50 dark:text-rose-100'
    case 'warning':
      return 'border-amber-300 bg-amber-50 text-amber-950 dark:border-amber-900/60 dark:bg-amber-950/40 dark:text-amber-100'
    default:
      return 'border-slate-300 bg-slate-50 text-slate-800 dark:border-slate-700 dark:bg-slate-950/70 dark:text-slate-100'
  }
}

export function NotificationCenter() {
  const { isAdmin } = useAuth()
  const [open, setOpen] = useState(false)
  const [items, setItems] = useState<ActiveAlertWire[]>([])
  const [err, setErr] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const rootRef = useRef<HTMLDivElement | null>(null)

  const refresh = useCallback(async () => {
    setLoading(true)
    setErr(null)
    try {
      const data = await fetchActiveAlerts({ limit: 40, includeResolved: true })
      setItems(Array.isArray(data) ? data : [])
    } catch (e) {
      setErr(e instanceof Error ? e.message : 'Failed to load alerts')
      setItems([])
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void refresh()
  }, [refresh])

  useEffect(() => {
    if (!open) {
      return
    }
    const onDoc = (ev: MouseEvent) => {
      if (!rootRef.current) {
        return
      }
      if (!rootRef.current.contains(ev.target as Node)) {
        setOpen(false)
      }
    }
    document.addEventListener('mousedown', onDoc)
    return () => document.removeEventListener('mousedown', onDoc)
  }, [open])

  const openCount = useMemo(
    () => items.filter((a) => a.resolved_at == null).length,
    [items],
  )

  async function onResolve(id: string) {
    try {
      await resolveActiveAlert(id)
      await refresh()
    } catch (e) {
      setErr(e instanceof Error ? e.message : 'Resolve failed')
    }
  }

  return (
    <div className="relative" ref={rootRef}>
      <button
        type="button"
        className={`relative flex size-8 items-center justify-center rounded border border-slate-200 bg-white text-slate-700 hover:bg-slate-100 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200 dark:hover:bg-slate-800`}
        title="Alerts"
        onClick={() => {
          setOpen((v) => !v)
          void refresh()
        }}
      >
        <Bell className="size-3.5" />
        {openCount > 0 ? (
          <span className="absolute -right-1 -top-1 min-w-4 rounded-full bg-rose-600 px-[3px] text-center font-mono text-[9px] font-semibold text-white shadow-sm">
            {openCount > 99 ? '99+' : openCount}
          </span>
        ) : null}
      </button>

      {open ? (
        <div className="absolute right-0 z-50 mt-2 w-[min(100vw-2rem,22rem)] overflow-hidden rounded border border-slate-200 bg-white shadow-lg dark:border-slate-700 dark:bg-slate-950">
          <div className="flex items-center justify-between border-b border-slate-200 px-3 py-2 dark:border-slate-800">
            <div className={`${shell.label} !tracking-normal normal-case text-slate-600 dark:text-slate-300`}>
              Notification Center
            </div>
            <button
              type="button"
              className={`text-[10px] ${shell.btnSecondary} px-2 py-0.5`}
              onClick={() => void refresh()}
            >
              {loading ? '…' : 'Refresh'}
            </button>
          </div>
          <div className="max-h-80 overflow-y-auto">
            {err ? (
              <div className={shell.warn + ' m-2'}>{err}</div>
            ) : null}
            {items.length === 0 && !loading ? (
              <div className="px-3 py-6 text-center text-[11px] text-slate-500">No alerts yet.</div>
            ) : null}
            <ul className="divide-y divide-slate-200 dark:divide-slate-800">
              {items.map((a) => {
                const openRow = !a.resolved_at
                return (
                  <li key={a.id} className="px-3 py-2.5">
                    <div className="flex flex-wrap items-start gap-2">
                      <span
                        className={`rounded border px-1.5 py-0 text-[10px] font-semibold uppercase ${severityChipClass(
                          String(a.severity ?? ''),
                        )}`}
                      >
                        {String(a.severity ?? '')}
                      </span>
                      {openRow ? (
                        <span className="rounded border border-amber-400/60 bg-amber-50 px-1.5 py-0 text-[9px] font-semibold uppercase text-amber-900 dark:bg-amber-950/60 dark:text-amber-100">
                          Open
                        </span>
                      ) : (
                        <span className="rounded border border-slate-300 px-1.5 py-0 text-[9px] font-semibold uppercase text-slate-500 dark:border-slate-700">
                          Cleared
                        </span>
                      )}
                      {isAdmin && openRow ? (
                        <button
                          type="button"
                          className={`ml-auto flex items-center gap-1 rounded px-2 py-0.5 text-[10px] ${shell.btnSecondary}`}
                          title="Acknowledge manually"
                          onClick={() => void onResolve(a.id)}
                        >
                          <CheckCircle2 className="size-3.5 opacity-70" />
                          Resolve
                        </button>
                      ) : null}
                    </div>
                    <div className="mt-1 text-xs font-semibold text-slate-900 dark:text-slate-50">
                      {a.title}
                    </div>
                    <div className="mt-1 text-[11px] leading-snug text-slate-600 dark:text-slate-400">{a.message}</div>
                    <div className="mt-2 font-mono text-[10px] text-slate-500">
                      {new Date(a.triggered_at).toLocaleString()}
                    </div>
                  </li>
                )
              })}
            </ul>
          </div>
        </div>
      ) : null}
    </div>
  )
}
