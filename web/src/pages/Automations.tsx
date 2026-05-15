import { useCallback, useEffect, useMemo, useState, type FormEvent } from 'react'
import { Clock, Loader2, Play, Power, RefreshCw } from 'lucide-react'
import { useWebSocket } from '../hooks/useWebSocket'
import { useAuth } from '../context/AuthContext'
import {
  createAutomation,
  fetchAutomations,
  patchAutomation,
  type AutomationRow,
} from '../lib/automationsApi'
import { fetchPackages, type PackageRow } from '../lib/packagesApi'

const OS_OPTIONS = [
  { value: 'windows', label: 'Windows' },
  { value: 'linux', label: 'Linux' },
  { value: 'android', label: 'Android' },
  { value: 'darwin', label: 'macOS (darwin)' },
  { value: 'ios', label: 'iOS' },
  { value: 'unknown', label: 'Unknown' },
]

export function AutomationsPage() {
  const { canOperate } = useAuth()
  const { assets } = useWebSocket()
  const [rows, setRows] = useState<AutomationRow[]>([])
  const [packages, setPackages] = useState<PackageRow[]>([])
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)

  const [name, setName] = useState('')
  const [cron, setCron] = useState('0 * * * *')
  const [action, setAction] = useState<'shutdown' | 'deploy_package'>('shutdown')
  const [targetMode, setTargetMode] = useState<'os' | 'asset'>('os')
  const [targetOs, setTargetOs] = useState('windows')
  const [targetAssetId, setTargetAssetId] = useState('')
  const [packageId, setPackageId] = useState('')

  const load = useCallback(async () => {
    setErr(null)
    setLoading(true)
    try {
      const [list, pkgs] = await Promise.all([
        fetchAutomations(),
        fetchPackages().catch(() => [] as PackageRow[]),
      ])
      setRows(list)
      setPackages(pkgs)
    } catch (e) {
      setErr(e instanceof Error ? e.message : 'failed to load')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const assetOptions = useMemo(() => {
    return assets
      .filter((a) => a.id)
      .map((a) => ({
        id: a.id as string,
        label: `${a.human_id} · ${a.hostname || '—'} (${a.os_type || a.os || '?'})`,
      }))
  }, [assets])

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    if (!canOperate) return
    setSaving(true)
    setErr(null)
    try {
      const body: Parameters<typeof createAutomation>[0] = {
        name,
        cron_schedule: cron,
        action_type: action,
        is_active: true,
      }
      if (targetMode === 'asset') {
        if (!targetAssetId) {
          setErr('Select a target asset.')
          setSaving(false)
          return
        }
        body.target_asset_id = targetAssetId
      } else {
        body.target_os = targetOs
      }
      if (action === 'deploy_package') {
        if (!packageId) {
          setErr('Select a package for deploy.')
          setSaving(false)
          return
        }
        body.payload_json = { package_id: packageId, operation: 'install' }
      } else {
        body.payload_json = {}
      }
      await createAutomation(body)
      setName('')
      await load()
    } catch (e) {
      setErr(e instanceof Error ? e.message : 'save failed')
    } finally {
      setSaving(false)
    }
  }

  async function toggleActive(row: AutomationRow) {
    if (!canOperate) return
    setErr(null)
    try {
      await patchAutomation(row.id, { is_active: !row.is_active })
      await load()
    } catch (e) {
      setErr(e instanceof Error ? e.message : 'update failed')
    }
  }

  return (
    <div className="min-h-full bg-slate-950 px-6 py-4">
      <div className="mb-4 flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-lg font-semibold tracking-tight text-slate-100">
            Automations
          </h1>
          <p className="mt-0.5 max-w-xl text-xs text-slate-500">
            Scheduled jobs use standard cron (e.g. <code className="text-slate-400">0 * * * *</code> hourly).
            Actions are sent over the agent C2 channel when targets are connected.
          </p>
        </div>
        <button
          type="button"
          className="inline-flex items-center gap-1.5 rounded border border-slate-700 px-2.5 py-1 text-[11px] text-slate-300 hover:bg-slate-800"
          onClick={() => void load()}
        >
          <RefreshCw className="size-3.5" />
          Refresh
        </button>
      </div>

      {err ? (
        <div className="mb-3 rounded border border-rose-900/60 bg-rose-950/40 px-3 py-2 font-mono text-[11px] text-rose-200">
          {err}
        </div>
      ) : null}

      <div className="grid gap-4 lg:grid-cols-2">
        <section className="rounded border border-slate-800 bg-slate-900/50 p-4">
          <h2 className="mb-3 flex items-center gap-2 text-xs font-semibold uppercase tracking-wide text-slate-400">
            <Clock className="size-3.5" />
            New schedule
          </h2>
          {!canOperate ? (
            <p className="text-xs text-slate-500">Operator role required to create automations.</p>
          ) : (
            <form className="space-y-3 text-[12px]" onSubmit={onSubmit}>
              <label className="block">
                <span className="mb-0.5 block text-[11px] text-slate-500">Name</span>
                <input
                  className="w-full rounded border border-slate-700 bg-slate-950 px-2 py-1.5 text-slate-100"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  required
                  maxLength={200}
                />
              </label>
              <label className="block">
                <span className="mb-0.5 block text-[11px] text-slate-500">Cron expression</span>
                <input
                  className="w-full rounded border border-slate-700 bg-slate-950 px-2 py-1.5 font-mono text-[11px] text-slate-100"
                  value={cron}
                  onChange={(e) => setCron(e.target.value)}
                  required
                />
              </label>
              <label className="block">
                <span className="mb-0.5 block text-[11px] text-slate-500">Action</span>
                <select
                  className="w-full rounded border border-slate-700 bg-slate-950 px-2 py-1.5 text-slate-100"
                  value={action}
                  onChange={(e) =>
                    setAction(e.target.value as 'shutdown' | 'deploy_package')
                  }
                >
                  <option value="shutdown">Shutdown host</option>
                  <option value="deploy_package">Deploy software</option>
                </select>
              </label>

              <div>
                <span className="mb-1 block text-[11px] text-slate-500">Target</span>
                <div className="mb-2 flex gap-3 text-slate-300">
                  <label className="inline-flex items-center gap-1.5">
                    <input
                      type="radio"
                      name="tm"
                      checked={targetMode === 'os'}
                      onChange={() => setTargetMode('os')}
                    />
                    By OS
                  </label>
                  <label className="inline-flex items-center gap-1.5">
                    <input
                      type="radio"
                      name="tm"
                      checked={targetMode === 'asset'}
                      onChange={() => setTargetMode('asset')}
                    />
                    Single asset
                  </label>
                </div>
                {targetMode === 'os' ? (
                  <select
                    className="w-full rounded border border-slate-700 bg-slate-950 px-2 py-1.5 text-slate-100"
                    value={targetOs}
                    onChange={(e) => setTargetOs(e.target.value)}
                  >
                    {OS_OPTIONS.map((o) => (
                      <option key={o.value} value={o.value}>
                        {o.label}
                      </option>
                    ))}
                  </select>
                ) : (
                  <select
                    className="w-full rounded border border-slate-700 bg-slate-950 px-2 py-1.5 text-slate-100"
                    value={targetAssetId}
                    onChange={(e) => setTargetAssetId(e.target.value)}
                  >
                    <option value="">Select asset…</option>
                    {assetOptions.map((a) => (
                      <option key={a.id} value={a.id}>
                        {a.label}
                      </option>
                    ))}
                  </select>
                )}
              </div>

              {action === 'deploy_package' ? (
                <label className="block">
                  <span className="mb-0.5 block text-[11px] text-slate-500">Package</span>
                  <select
                    className="w-full rounded border border-slate-700 bg-slate-950 px-2 py-1.5 text-slate-100"
                    value={packageId}
                    onChange={(e) => setPackageId(e.target.value)}
                  >
                    <option value="">Select package…</option>
                    {packages.map((p) => (
                      <option key={p.id} value={p.id}>
                        {p.name} ({p.type}) {p.version}
                      </option>
                    ))}
                  </select>
                </label>
              ) : null}

              <button
                type="submit"
                disabled={saving}
                className="inline-flex items-center gap-2 rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-500 disabled:opacity-50"
              >
                {saving ? (
                  <Loader2 className="size-3.5 animate-spin" />
                ) : (
                  <Play className="size-3.5" />
                )}
                Create automation
              </button>
            </form>
          )}
        </section>

        <section className="rounded border border-slate-800 bg-slate-900/50 p-4">
          <h2 className="mb-3 text-xs font-semibold uppercase tracking-wide text-slate-400">
            Active schedules
          </h2>
          {loading ? (
            <div className="flex items-center gap-2 text-xs text-slate-500">
              <Loader2 className="size-4 animate-spin" />
              Loading…
            </div>
          ) : rows.length === 0 ? (
            <p className="text-xs text-slate-500">No automations yet.</p>
          ) : (
            <ul className="space-y-2 text-[12px]">
              {rows.map((row) => (
                <li
                  key={row.id}
                  className="flex flex-wrap items-start justify-between gap-2 rounded border border-slate-800/80 bg-slate-950/60 px-2.5 py-2"
                >
                  <div className="min-w-0">
                    <div className="font-medium text-slate-100">{row.name}</div>
                    <div className="mt-0.5 font-mono text-[10px] text-slate-500">
                      {row.cron_schedule} · {row.action_type}
                    </div>
                    <div className="mt-0.5 text-[10px] text-slate-500">
                      {row.target_asset_id
                        ? `asset ${row.target_asset_id}`
                        : `os ${row.target_os ?? '—'}`}
                      {row.is_active ? (
                        <span className="ml-2 text-emerald-500/90">active</span>
                      ) : (
                        <span className="ml-2 text-slate-600">paused</span>
                      )}
                    </div>
                  </div>
                  {canOperate ? (
                    <button
                      type="button"
                      className="shrink-0 rounded border border-slate-700 px-2 py-1 text-[10px] text-slate-300 hover:bg-slate-800"
                      onClick={() => void toggleActive(row)}
                    >
                      {row.is_active ? 'Pause' : 'Resume'}
                    </button>
                  ) : null}
                </li>
              ))}
            </ul>
          )}
        </section>
      </div>

      <p className="mt-4 flex items-start gap-2 text-[11px] leading-relaxed text-slate-600">
        <Power className="mt-0.5 size-3.5 shrink-0 text-slate-500" />
        Shutdown and deploy actions mirror manual C2 commands: agents must be connected and enrolled with a
        certificate serial. OS targets include every matching asset that has a cert serial on file.
      </p>
    </div>
  )
}
