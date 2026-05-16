import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { ArchiveRestore, ArrowLeft, RefreshCw } from 'lucide-react'
import { Navigate } from 'react-router-dom'
import { useAuth } from '../context/AuthContext'
import { backupDownloadHref, fetchBackups, triggerBackup, type BackupWire } from '../lib/backupsApi'
import { formatBytes } from '../lib/format'
import { dashboardFetch } from '../lib/ticketsApi'
import { shell } from '../lib/themeClasses'

function isoLocal(iso: string): string {
  const d = Date.parse(iso)
  if (!Number.isFinite(d)) {
    return iso
  }
  return new Date(d).toLocaleString(undefined, {
    dateStyle: 'medium',
    timeStyle: 'short',
  })
}

async function blobDownload(filename: string) {
  const res = await dashboardFetch(backupDownloadHref(filename))
  if (!res.ok) {
    const j = (await res.json().catch(() => null)) as { error?: string } | null
    throw new Error(j?.error ?? res.statusText)
  }
  const blob = await res.blob()
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  a.rel = 'noopener'
  document.body.appendChild(a)
  a.click()
  a.remove()
  URL.revokeObjectURL(url)
}

export function BackupSettingsPage() {
  const { isAdmin } = useAuth()
  const [rows, setRows] = useState<BackupWire[]>([])
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState<string | null>(null)
  const [msg, setMsg] = useState<string | null>(null)
  const [backingUp, setBackingUp] = useState(false)

  const load = useCallback(async () => {
    setErr(null)
    setLoading(true)
    try {
      setRows(await fetchBackups())
    } catch (e) {
      setErr(e instanceof Error ? e.message : 'Failed loading backups')
      setRows([])
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  async function manualBackup() {
    setBackingUp(true)
    setMsg(null)
    setErr(null)
    try {
      await triggerBackup()
      setMsg('Backup completed successfully.')
      await load()
    } catch (e) {
      setErr(e instanceof Error ? e.message : 'Backup trigger failed')
    } finally {
      setBackingUp(false)
    }
  }

  async function downloadRow(fname: string) {
    try {
      setErr(null)
      await blobDownload(fname)
    } catch (e) {
      setErr(e instanceof Error ? e.message : 'Download failed')
    }
  }

  if (!isAdmin) {
    return <Navigate to="/" replace />
  }

  return (
    <div className="p-6 text-slate-900 dark:text-slate-100">
      <div className="mb-4 flex flex-wrap items-center gap-2 text-[11px] text-slate-500">
        <ArrowLeft className="size-3.5 shrink-0" />
        <Link className="text-sky-600 hover:underline dark:text-sky-400" to="/settings">
          Settings
        </Link>
        <span className="text-slate-400">/</span>
        <span className="text-slate-600 dark:text-slate-400">Disaster recovery</span>
      </div>

      <div className="flex flex-wrap items-start gap-4">
        <div className="mt-0.5 rounded border border-slate-300 dark:border-slate-700 bg-slate-900/80 p-2">
          <ArchiveRestore className="size-5 text-sky-400/90" />
        </div>
        <div className="min-w-[220px] flex-1">
          <h1 className="text-lg font-semibold">Backup bundles</h1>
          <p className="mt-1 max-w-2xl text-xs text-slate-500">
            Offline archives bundle a PostgreSQL custom-format dump (<span className="font-mono">postgres/database.dump</span>) alongside a verbatim copy of embedded PKI
            (<span className="font-mono">embedded_pki/**</span>). Automation runs nightly (UTC cron from{' '}
            <span className="font-mono">ARX_BACKUP_CRON_SPEC</span>, default <span className="font-mono">0 2 * * *</span>) and rotates anything older than the retention window configured on the
            server.
          </p>
        </div>
        <button
          type="button"
          disabled={backingUp || loading}
          className={`ml-auto shrink-0 ${shell.btnPrimary} inline-flex items-center gap-2`}
          onClick={() => void manualBackup()}
        >
          {backingUp ? 'Working…' : 'Trigger manual backup'}
        </button>
        <button
          type="button"
          disabled={loading || backingUp}
          title="Reload list"
          className={`shrink-0 ${shell.btnSecondary} inline-flex items-center gap-1.5`}
          onClick={() => void load()}
        >
          <RefreshCw className="size-3.5 opacity-70" />
          Refresh
        </button>
      </div>

      {err ? <div className={`mt-5 ${shell.error}`}>{err}</div> : null}
      {msg ? (
        <div className="mt-5 rounded border border-emerald-600/70 bg-emerald-950/20 px-3 py-2 text-xs text-emerald-200">
          {msg}
        </div>
      ) : null}

      <section className="mt-8">
        <div className={shell.tableWrap}>
          <table className="w-full border-collapse text-left text-[11px]">
            <thead className="bg-slate-200/70 text-[10px] uppercase tracking-wide dark:bg-slate-900">
              <tr>
                <th className="px-3 py-2 font-semibold">Archive</th>
                <th className="px-3 py-2 font-semibold">Captured at</th>
                <th className="px-3 py-2 font-semibold">Disk size</th>
                <th className="px-3 py-2 font-semibold text-right">Download</th>
              </tr>
            </thead>
            <tbody>
              {loading ? (
                <tr>
                  <td colSpan={4} className="px-3 py-8 text-xs text-slate-500">
                    Loading snapshots…
                  </td>
                </tr>
              ) : rows.length === 0 ? (
                <tr>
                  <td colSpan={4} className="px-3 py-8 text-xs text-slate-500">
                    No snapshots yet — trigger one manually or wait for cron.
                  </td>
                </tr>
              ) : (
                rows.map((row) => (
                  <tr key={row.filename} className="border-t border-slate-200 dark:border-slate-800">
                    <td className="px-3 py-2 align-middle font-mono text-[10px]">{row.filename}</td>
                    <td className="px-3 py-2 align-middle text-slate-400">{isoLocal(row.created_at)}</td>
                    <td className="px-3 py-2 align-middle font-mono text-[10px] text-slate-300">
                      {formatBytes(row.size_bytes)}
                    </td>
                    <td className="px-3 py-2 align-middle text-right">
                      <button
                        type="button"
                        className={`${shell.btnSecondary} inline-flex`}
                        title={`Download ${row.filename}`}
                        onClick={() => void downloadRow(row.filename)}
                      >
                        Save to disk
                      </button>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </section>
    </div>
  )
}
