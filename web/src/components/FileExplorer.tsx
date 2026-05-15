import { useCallback, useEffect, useMemo, useRef, useState, type ChangeEvent } from 'react'
import { ChevronRight, Download, FolderOpen, RefreshCw, Upload } from 'lucide-react'
import type { AgentUplinkMessage, FsListDirResultMessage } from '../types/ws'

export type DirEntryRow = {
  name: string
  is_dir: boolean
  size: number
  mod_time_unix: number
  mode_perm_octal: number
}

function dashboardToken(): string {
  return import.meta.env.VITE_ARX_DASHBOARD_TOKEN?.trim() ?? ''
}

function joinPath(base: string, name: string): string {
  const b = base.replace(/[/\\]+$/, '')
  if (!b) {
    return name
  }
  const sep = base.includes('\\') ? '\\' : '/'
  return `${b}${sep}${name}`
}

function parentPath(p: string): string {
  const s = p.replace(/[/\\]+$/, '')
  if (!s) {
    return '/'
  }
  if (/^[a-zA-Z]:$/.test(s)) {
    return `${s}\\`
  }
  const idx = Math.max(s.lastIndexOf('/'), s.lastIndexOf('\\'))
  if (idx <= 0) {
    if (/^[a-zA-Z]:\\/.test(s)) {
      return s.slice(0, 3)
    }
    return '/'
  }
  return s.slice(0, idx) || '/'
}

function hasParentDir(p: string): boolean {
  const t = p.replace(/[/\\]+$/, '')
  if (t === '' || t === '/') {
    return false
  }
  if (/^[a-zA-Z]:\\?$/.test(t)) {
    return false
  }
  return true
}

function breadcrumbParts(p: string): { label: string; full: string }[] {
  if (!p || p === '/') {
    return [{ label: '/', full: '/' }]
  }
  if (/^[a-zA-Z]:\\/.test(p)) {
    const bits = p.split(/\\+/).filter(Boolean)
    const out: { label: string; full: string }[] = []
    let acc = ''
    for (let i = 0; i < bits.length; i++) {
      acc = i === 0 ? `${bits[0]}\\` : `${acc.replace(/\\$/, '')}\\${bits[i]}`
      out.push({ label: bits[i]!, full: acc })
    }
    return out
  }
  const bits = p.split('/').filter(Boolean)
  if (p.startsWith('/')) {
    let acc = ''
    return bits.map((b) => {
      acc = `${acc}/${b}`
      return { label: b, full: acc }
    })
  }
  let acc = ''
  return bits.map((b) => {
    acc = acc ? `${acc}/${b}` : b
    return { label: b, full: acc }
  })
}

type Props = {
  assetId: string
  humanId: string
  c2Connected: boolean
  sendJson: (payload: Record<string, unknown>) => void
  subscribeAgentUplink: (handler: (msg: AgentUplinkMessage) => void) => () => void
  initialPath?: string
}

export function FileExplorer({
  assetId,
  humanId,
  c2Connected,
  sendJson,
  subscribeAgentUplink,
  initialPath = '/',
}: Props) {
  const [path, setPath] = useState(initialPath)
  const [entries, setEntries] = useState<DirEntryRow[]>([])
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const listRequestId = useRef<string | null>(null)

  const listDir = useCallback(
    (target: string) => {
      if (!c2Connected) {
        setError('Agent is offline')
        return
      }
      setBusy(true)
      setError(null)
      const request_id =
        typeof crypto !== 'undefined' && crypto.randomUUID
          ? crypto.randomUUID()
          : String(Date.now())
      listRequestId.current = request_id
      sendJson({
        action: 'fs_listdir',
        target_arx_id: humanId,
        path: target,
        request_id,
      })
    },
    [c2Connected, humanId, sendJson],
  )

  useEffect(() => {
    listDir(path)
  }, [path, listDir])

  useEffect(() => {
    return subscribeAgentUplink((msg: AgentUplinkMessage) => {
      if (msg.type !== 'fs_listdir_result') {
        return
      }
      const m = msg as FsListDirResultMessage
      if (m.request_id !== listRequestId.current) {
        return
      }
      setBusy(false)
      if (!m.ok) {
        setError(m.error ?? 'listdir failed')
        setEntries([])
        return
      }
      setEntries((m.entries as DirEntryRow[]) ?? [])
    })
  }, [subscribeAgentUplink])

  const crumbs = useMemo(() => breadcrumbParts(path), [path])

  const onUploadPick = async (ev: ChangeEvent<HTMLInputElement>) => {
    const file = ev.target.files?.[0]
    ev.target.value = ''
    if (!file || !assetId) {
      return
    }
    const dest = joinPath(path, file.name)
    const urlPath = encodeURIComponent(dest)
    const tok = dashboardToken()
    const auth = tok
      ? `?token=${encodeURIComponent(tok)}&path=${urlPath}`
      : `?path=${urlPath}`
    setBusy(true)
    setError(null)
    try {
      const res = await fetch(
        `/v1/assets/${encodeURIComponent(assetId)}/files/upload${auth}`,
        {
          method: 'POST',
          headers: { 'Content-Type': 'application/octet-stream' },
          body: file,
        },
      )
      if (!res.ok) {
        const j = (await res.json().catch(() => null)) as { error?: string } | null
        setError(j?.error ?? `upload failed (${res.status})`)
      } else {
        listDir(path)
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : 'upload failed')
    } finally {
      setBusy(false)
    }
  }

  const downloadFile = async (name: string) => {
    const remote = joinPath(path, name)
    const urlPath = encodeURIComponent(remote)
    const tok = dashboardToken()
    const auth = tok
      ? `?token=${encodeURIComponent(tok)}&path=${urlPath}`
      : `?path=${urlPath}`
    setBusy(true)
    setError(null)
    try {
      const res = await fetch(
        `/v1/assets/${encodeURIComponent(assetId)}/files/download${auth}`,
      )
      if (!res.ok) {
        const j = (await res.json().catch(() => null)) as { error?: string } | null
        setError(j?.error ?? `download failed (${res.status})`)
        return
      }
      const blob = await res.blob()
      const a = document.createElement('a')
      a.href = URL.createObjectURL(blob)
      a.download = name
      a.click()
      URL.revokeObjectURL(a.href)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'download failed')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap items-center gap-2 text-[11px] text-slate-400">
        <span className="font-semibold uppercase tracking-wide text-slate-500">
          Files
        </span>
        <button
          type="button"
          disabled={!c2Connected || busy}
          onClick={() => listDir(path)}
          className="inline-flex items-center gap-1 rounded border border-slate-300 dark:border-slate-700 px-2 py-0.5 text-slate-700 hover:bg-slate-200 dark:text-slate-300 dark:hover:bg-slate-800 disabled:opacity-40"
        >
          <RefreshCw className="size-3" />
          Refresh
        </button>
        <label className="inline-flex cursor-pointer items-center gap-1 rounded border border-slate-300 dark:border-slate-700 px-2 py-0.5 text-slate-700 hover:bg-slate-200 dark:text-slate-300 dark:hover:bg-slate-800 disabled:opacity-40">
          <Upload className="size-3" />
          Upload
          <input
            type="file"
            className="hidden"
            disabled={!c2Connected || busy || !assetId}
            onChange={onUploadPick}
          />
        </label>
      </div>

      <nav className="flex flex-wrap items-center gap-0.5 text-[11px] text-slate-500">
        {crumbs.map((c, i) => (
          <span key={`${c.full}-${i}`} className="inline-flex items-center gap-0.5">
            {i > 0 ? <ChevronRight className="size-3 text-slate-600" /> : null}
            <button
              type="button"
              className="rounded px-1 py-0.5 hover:bg-slate-800 hover:text-slate-200"
              onClick={() => setPath(c.full)}
            >
              {c.label}
            </button>
          </span>
        ))}
      </nav>

      {error ? (
        <div className="rounded border border-rose-900/60 bg-rose-950/30 px-2 py-1.5 text-[11px] text-rose-800 dark:text-rose-200">
          {error}
        </div>
      ) : null}

      <div className="overflow-x-auto rounded border border-slate-200 dark:border-slate-800">
        <table className="w-full border-collapse text-left text-[11px]">
          <thead className="bg-slate-900/80 text-slate-500">
            <tr>
              <th className="border-b border-slate-200 dark:border-slate-800 px-2 py-2">Name</th>
              <th className="border-b border-slate-200 dark:border-slate-800 px-2 py-2">Size</th>
              <th className="border-b border-slate-200 dark:border-slate-800 px-2 py-2"> </th>
            </tr>
          </thead>
          <tbody className="text-slate-300">
            {hasParentDir(path) ? (
              <tr className="border-b border-slate-200 dark:border-slate-800/80">
                <td className="px-2 py-1.5" colSpan={3}>
                  <button
                    type="button"
                    className="inline-flex items-center gap-1 text-sky-400 hover:text-sky-300"
                    onClick={() => setPath(parentPath(path))}
                  >
                    <FolderOpen className="size-3.5" />..
                  </button>
                </td>
              </tr>
            ) : null}
            {entries.map((e) => (
              <tr key={e.name} className="border-b border-slate-200 dark:border-slate-800/80">
                <td className="px-2 py-1.5">
                  {e.is_dir ? (
                    <button
                      type="button"
                      className="inline-flex items-center gap-1 text-sky-400 hover:text-sky-300"
                      onClick={() => setPath(joinPath(path, e.name))}
                    >
                      <FolderOpen className="size-3.5" />
                      {e.name}
                    </button>
                  ) : (
                    <span>{e.name}</span>
                  )}
                </td>
                <td className="px-2 py-1.5 font-mono text-slate-500">
                  {e.is_dir ? '—' : e.size}
                </td>
                <td className="whitespace-nowrap px-2 py-1.5 text-right">
                  {!e.is_dir && assetId ? (
                    <button
                      type="button"
                      className="inline-flex items-center gap-1 text-sky-400 hover:text-sky-300 disabled:opacity-40"
                      disabled={busy}
                      onClick={() => downloadFile(e.name)}
                    >
                      <Download className="size-3" />
                      Download
                    </button>
                  ) : null}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        {entries.length === 0 && !busy ? (
          <div className="px-3 py-4 text-[11px] text-slate-600">Empty directory</div>
        ) : null}
        {busy ? (
          <div className="px-3 py-2 text-[11px] text-slate-500">Working…</div>
        ) : null}
      </div>
      {!assetId ? (
        <p className="text-[11px] text-amber-500/90">
          Asset UUID is not available yet; upload/download requires the catalog id field. Wait for
          the next telemetry update or refresh the dashboard.
        </p>
      ) : null}
    </div>
  )
}
