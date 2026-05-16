import { useCallback, useEffect, useState, type FormEvent } from 'react'
import { useAuth } from '../context/AuthContext'
import {
  deleteAppCatalog,
  fetchAppCatalog,
  patchAppCatalog,
  uploadAppCatalogEntry,
  createCatalogFromURL,
  type AppCatalogRow,
} from '../lib/appCatalogApi'

const OS_OPTS = ['windows', 'linux', 'android'] as const

export function AppCatalogPage() {
  const { canOperate } = useAuth()
  const [rows, setRows] = useState<AppCatalogRow[]>([])
  const [err, setErr] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [name, setName] = useState('')
  const [version, setVersion] = useState('')
  const [targetOs, setTargetOs] = useState<(typeof OS_OPTS)[number]>('windows')
  const [installArgs, setInstallArgs] = useState('')
  const [file, setFile] = useState<File | null>(null)
  const [extUrl, setExtUrl] = useState('')
  const [metaMode, setMetaMode] = useState<'upload' | 'url'>('upload')

  const reload = useCallback(async () => {
    setErr(null)
    const list = await fetchAppCatalog()
    setRows(list)
  }, [])

  useEffect(() => {
    reload().catch((e) => setErr(e instanceof Error ? e.message : String(e)))
  }, [reload])

  async function onUpload(ev: FormEvent) {
    ev.preventDefault()
    if (!file) return
    setBusy(true)
    setErr(null)
    try {
      const fd = new FormData()
      fd.set('name', name.trim())
      fd.set('version', version.trim())
      fd.set('target_os', targetOs)
      fd.set('install_args', installArgs.trim())
      fd.set('file', file)
      await uploadAppCatalogEntry(fd)
      setName('')
      setVersion('')
      setInstallArgs('')
      setFile(null)
      await reload()
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  async function onMetaUrl(ev: FormEvent) {
    ev.preventDefault()
    setBusy(true)
    setErr(null)
    try {
      await createCatalogFromURL({
        name: name.trim(),
        version: version.trim(),
        target_os: targetOs,
        file_path_or_url: extUrl.trim(),
        install_args: installArgs.trim(),
      })
      setExtUrl('')
      setName('')
      setVersion('')
      setInstallArgs('')
      await reload()
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  async function onDelete(id: string) {
    if (!window.confirm('Delete this catalog row and staged blob when applicable?'))
      return
    setBusy(true)
    try {
      await deleteAppCatalog(id)
      await reload()
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="min-h-full bg-slate-50 px-4 py-4 md:px-6 dark:bg-slate-950">
      <h1 className="mb-1 text-lg font-semibold text-slate-900 dark:text-slate-50">
        App Catalog
      </h1>
      <p className="mb-6 max-w-xl text-[12px] text-slate-500">
        Upload deployment artifacts (Windows/Linux/Android targets) stored under the
        server apps volume or register HTTPS-hosted installers.
      </p>

      <div className="mb-4 flex flex-wrap gap-2 border-b border-slate-200 pb-3 dark:border-slate-800">
        <button
          type="button"
          className={`rounded px-2.5 py-1 text-[11px] font-medium ring-1 ${
            metaMode === 'upload'
              ? 'bg-sky-950/70 text-sky-50 ring-sky-800'
              : 'text-slate-500 ring-transparent hover:text-slate-800 dark:hover:text-slate-300'
          }`}
          onClick={() => setMetaMode('upload')}
        >
          Binary upload
        </button>
        <button
          type="button"
          className={`rounded px-2.5 py-1 text-[11px] font-medium ring-1 ${
            metaMode === 'url'
              ? 'bg-sky-950/70 text-sky-50 ring-sky-800'
              : 'text-slate-500 ring-transparent hover:text-slate-800 dark:hover:text-slate-300'
          }`}
          onClick={() => setMetaMode('url')}
        >
          External HTTPS URL only
        </button>
      </div>

      {err ? (
        <div className="mb-4 rounded border border-rose-900/60 bg-rose-950/30 px-3 py-2 text-[12px] text-rose-200">
          {err}
        </div>
      ) : null}

      {canOperate && metaMode === 'upload' ? (
        <form
          onSubmit={onUpload}
          className="mb-8 max-w-lg space-y-2 rounded border border-slate-200 bg-white/95 p-4 text-[12px] dark:border-slate-800 dark:bg-slate-900/40"
        >
          <div className="text-[10px] font-semibold uppercase text-slate-500">
            New catalog entry — upload artifact
          </div>
          <input
            className="block w-full rounded border border-slate-300 px-2 py-1 dark:border-slate-700 dark:bg-slate-950"
            placeholder="Name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            required
          />
          <input
            className="block w-full rounded border border-slate-300 px-2 py-1 dark:border-slate-700 dark:bg-slate-950"
            placeholder="Version (optional)"
            value={version}
            onChange={(e) => setVersion(e.target.value)}
          />
          <select
            className="block w-full rounded border border-slate-300 px-2 py-1 dark:border-slate-700 dark:bg-slate-950"
            value={targetOs}
            onChange={(e) =>
              setTargetOs(e.target.value as (typeof OS_OPTS)[number])
            }
          >
            {OS_OPTS.map((o) => (
              <option key={o} value={o}>
                {o}
              </option>
            ))}
          </select>
          <input
            className="block w-full rounded border border-slate-300 px-2 py-1 dark:border-slate-700 dark:bg-slate-950"
            placeholder="Silent install_args (passed to installers on desktops)"
            value={installArgs}
            onChange={(e) => setInstallArgs(e.target.value)}
          />
          <input
            type="file"
            accept="application/*,.apk,.msi,.exe,.deb,.rpm,.pkg"
            required
            onChange={(e) => setFile(e.target.files?.[0] ?? null)}
          />
          <button
            type="submit"
            disabled={busy || !file}
            className="rounded bg-sky-700 px-3 py-2 text-[12px] font-medium text-white hover:bg-sky-600 disabled:opacity-40"
          >
            Upload and register
          </button>
        </form>
      ) : null}

      {canOperate && metaMode === 'url' ? (
        <form
          onSubmit={onMetaUrl}
          className="mb-8 max-w-lg space-y-2 rounded border border-slate-200 bg-white/95 p-4 text-[12px] dark:border-slate-800 dark:bg-slate-900/40"
        >
          <div className="text-[10px] font-semibold uppercase text-slate-500">
            HTTPS URL-only catalog metadata
          </div>
          <input
            className="block w-full rounded border border-slate-300 px-2 py-1 dark:border-slate-700 dark:bg-slate-950"
            placeholder="Name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            required
          />
          <input
            className="block w-full rounded border border-slate-300 px-2 py-1 dark:border-slate-700 dark:bg-slate-950"
            placeholder="Version (optional)"
            value={version}
            onChange={(e) => setVersion(e.target.value)}
          />
          <select
            className="block w-full rounded border border-slate-300 px-2 py-1 dark:border-slate-700 dark:bg-slate-950"
            value={targetOs}
            onChange={(e) =>
              setTargetOs(e.target.value as (typeof OS_OPTS)[number])
            }
          >
            {OS_OPTS.map((o) => (
              <option key={o} value={o}>
                {o}
              </option>
            ))}
          </select>
          <input
            className="block w-full rounded border border-slate-300 px-2 py-1 dark:border-slate-700 dark:bg-slate-950"
            placeholder="https:// CDN or mirror URL served over TLS"
            value={extUrl}
            onChange={(e) => setExtUrl(e.target.value)}
            required
          />
          <input
            className="block w-full rounded border border-slate-300 px-2 py-1 dark:border-slate-700 dark:bg-slate-950"
            placeholder="install_args (desktop agents)"
            value={installArgs}
            onChange={(e) => setInstallArgs(e.target.value)}
          />
          <button
            type="submit"
            disabled={busy}
            className="rounded bg-sky-700 px-3 py-2 text-[12px] font-medium text-white hover:bg-sky-600 disabled:opacity-40"
          >
            Register URL
          </button>
        </form>
      ) : null}

      <div className="min-w-0 overflow-x-auto rounded border border-slate-200 dark:border-slate-800">
        <table className="min-w-[640px] w-full border-collapse text-left text-[11px]">
          <thead className="bg-slate-900/85 text-slate-500">
            <tr>
              <th className="border-b px-2 py-2">Name</th>
              <th className="border-b px-2 py-2">Version</th>
              <th className="border-b px-2 py-2">OS</th>
              <th className="border-b px-2 py-2">Path / URL</th>
              <th className="border-b px-2 py-2">Created</th>
              {canOperate ? <th className="border-b px-2 py-2">Actions</th> : null}
            </tr>
          </thead>
          <tbody className="text-slate-300">
            {rows.map((r) => (
              <tr
                key={r.id}
                className="border-b border-slate-200 dark:border-slate-800/80"
              >
                <td className="px-2 py-1.5 font-medium">{r.name}</td>
                <td className="px-2 py-1.5 font-mono">{r.version}</td>
                <td className="px-2 py-1.5 capitalize">{r.target_os}</td>
                <td className="max-w-[240px] truncate px-2 py-1.5 font-mono text-[10px] text-sky-400/90">
                  {r.file_path_or_url}
                </td>
                <td className="px-2 py-1.5 text-slate-500">
                  {new Date(r.created_at).toLocaleString()}
                </td>
                {canOperate ? (
                  <td className="min-w-[140px] whitespace-nowrap px-2 py-1.5 align-top">
                    <CatalogRowQuickEdit row={r} onDone={reload} />
                    <button
                      type="button"
                      className="mt-2 text-rose-400 hover:text-rose-300 disabled:opacity-40"
                      disabled={busy}
                      onClick={() => void onDelete(r.id)}
                    >
                      Delete
                    </button>
                  </td>
                ) : null}
              </tr>
            ))}
          </tbody>
        </table>
        {rows.length === 0 ? (
          <div className="px-3 py-6 text-[12px] text-slate-600">
            No catalog entries yet.
          </div>
        ) : null}
      </div>

      {!canOperate ? (
        <p className="mt-6 text-[12px] text-slate-600">
          You have read-only access; catalog mutations are restricted to operators or
          administrators.
        </p>
      ) : null}
    </div>
  )
}

function CatalogRowQuickEdit({
  row,
  onDone,
}: {
  row: AppCatalogRow
  onDone: () => void
}) {
  const [name, setName] = useState(row.name)
  const [version, setVersion] = useState(row.version)
  const [busy, setBusy] = useState(false)
  async function save() {
    setBusy(true)
    try {
      await patchAppCatalog(row.id, {
        name: name.trim() || undefined,
        version,
      })
      await onDone()
    } finally {
      setBusy(false)
    }
  }
  return (
    <div className="flex flex-col gap-1">
      <input
        className="w-full rounded border border-slate-600 bg-slate-950 px-1 py-0.5 font-sans text-[10px]"
        value={name}
        onChange={(e) => setName(e.target.value)}
      />
      <input
        className="w-full rounded border border-slate-600 bg-slate-950 px-1 py-0.5 font-mono text-[10px]"
        value={version}
        onChange={(e) => setVersion(e.target.value)}
      />
      <button
        type="button"
        disabled={busy}
        onClick={() => void save()}
        className="rounded bg-emerald-800 px-2 py-0.5 text-[10px] text-emerald-100 hover:bg-emerald-700 disabled:opacity-40"
      >
        Save row
      </button>
    </div>
  )
}
