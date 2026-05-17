import { useEffect, useMemo, useState } from 'react'
import {
  fetchDeviceEffectivePolicy,
  type EffectivePolicySnapshot,
} from '../../lib/deviceEffectivePolicyApi'
import { shell } from '../../lib/themeClasses'

function formatValue(value: unknown): string {
  if (value === null || value === undefined) {
    return '—'
  }
  if (typeof value === 'object') {
    try {
      return JSON.stringify(value)
    } catch {
      return String(value)
    }
  }
  return String(value)
}

export function EffectivePoliciesTab(props: { deviceId: string }) {
  const [snap, setSnap] = useState<EffectivePolicySnapshot | null>(null)
  const [err, setErr] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  const reload = useMemo(
    () => async () => {
      setBusy(true)
      setErr(null)
      try {
        const next = await fetchDeviceEffectivePolicy(props.deviceId)
        setSnap(next)
      } catch (e) {
        setSnap(null)
        setErr(e instanceof Error ? e.message : String(e))
      } finally {
        setBusy(false)
      }
    },
    [props.deviceId],
  )

  useEffect(() => {
    void reload()
  }, [reload])

  return (
    <div className={`flex flex-col gap-4 ${shell.contentCompact}`}>
      <div className="flex flex-wrap items-center gap-3 border-b border-gray-200 pb-3 dark:border-neutral-900">
        <div>
          <div className={shell.label}>Effective policies</div>
          <p className={`mt-1 ${shell.body}`}>
            Restrictive merged view of every assigned configuration profile for this device&apos;s
            platform.
          </p>
        </div>
        <button
          type="button"
          className={`ml-auto shrink-0 rounded-xl border border-gray-300 px-3 py-1.5 text-[11px] font-semibold text-gray-800 hover:bg-gray-50 disabled:opacity-50 dark:border-neutral-700 dark:text-gray-200 dark:hover:bg-neutral-900`}
          disabled={busy}
          onClick={() => void reload()}
        >
          Refresh
        </button>
      </div>

      {snap?.has_conflict ? (
        <div
          className={`${shell.warn}`}
          style={{ boxShadow: 'none' }}
        >
          One or more settings required restrictive conflict resolution between profiles. Review the
          flagged rows below.
        </div>
      ) : null}

      {err ? (
        <div className={shell.error} style={{ boxShadow: 'none' }}>
          {err}
        </div>
      ) : null}

      {!snap && !err && busy ? (
        <div className={`${shell.muted}`}>Loading merged policy…</div>
      ) : null}

      {snap ? (
        <div className="flex flex-col gap-2">
          <div className={`flex flex-wrap gap-4 ${shell.muted}`}>
            <span>
              Platform <span className="font-semibold text-gray-800 dark:text-gray-100">{snap.platform}</span>
            </span>
            <span>
              Revision{' '}
              <span className="font-mono text-[11px] text-gray-800 dark:text-gray-100">
                {snap.revision.slice(0, 16)}…
              </span>
            </span>
          </div>

          <div className="space-y-2">
            {snap.settings.map((row) => (
              <div
                key={row.path}
                className={`flex flex-wrap items-start gap-3 ${shell.listItem}`}
                style={{ boxShadow: 'none' }}
              >
                <div className="min-w-[160px] flex-1 font-mono text-[11px] text-gray-900 dark:text-gray-100">
                  {row.path}
                </div>
                <div className="min-w-[140px] flex-[2] text-[12px] text-gray-800 dark:text-gray-200">
                  {formatValue(row.value)}
                </div>
                <div className="flex min-w-[120px] flex-col items-end gap-1 text-right">
                  {row.conflict ? (
                    <span
                      className="rounded-lg border border-amber-400 bg-amber-50 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-amber-950 dark:border-amber-700 dark:bg-amber-950/35 dark:text-amber-50"
                      style={{ boxShadow: 'none' }}
                    >
                      Conflict
                    </span>
                  ) : (
                    <span className={`${shell.muted}`}>Aligned</span>
                  )}
                  {row.conflict && row.source_profiles?.length ? (
                    <span className={`max-w-[240px] text-[10px] text-amber-900 dark:text-amber-100`}>
                      {row.source_profiles.map((p) => p.name).join(', ')}
                    </span>
                  ) : null}
                </div>
              </div>
            ))}
          </div>

          {snap.settings.length === 0 ? (
            <div className={`${shell.cardMuted} px-3 py-4 text-[12px]`} style={{ boxShadow: 'none' }}>
              No declarative payload keys resolved for this device.
            </div>
          ) : null}
        </div>
      ) : null}
    </div>
  )
}
