import { useCallback, useEffect, useMemo, useState } from 'react'
import { Navigate } from 'react-router-dom'
import { ClipboardList } from 'lucide-react'
import { useAuth } from '../context/AuthContext'
import { dashboardFetch } from '../lib/ticketsApi'

type AuditRow = {
  id: string
  timestamp: string
  user_id?: string
  username?: string
  action: string
  resource_type?: string
  resource_id?: string
  target_asset_id?: string
  target_human_id?: string
  ip_address?: string
  details?: unknown
}

type AuditListResponse = {
  items: AuditRow[]
  total: number
}

type UserOption = { id: string; username: string }

function formatTs(iso: string) {
  try {
    const d = new Date(iso)
    return d.toLocaleString(undefined, {
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
    })
  } catch {
    return iso
  }
}

function detailsPreview(d: unknown): string {
  if (d == null) return '—'
  try {
    const s = JSON.stringify(d)
    return s.length > 160 ? `${s.slice(0, 157)}…` : s
  } catch {
    return '—'
  }
}

export function AuditLogsPage() {
  const { isAdmin } = useAuth()
  const [rows, setRows] = useState<AuditRow[]>([])
  const [total, setTotal] = useState(0)
  const [err, setErr] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [offset, setOffset] = useState(0)
  const limit = 50

  const [fromDate, setFromDate] = useState('')
  const [toDate, setToDate] = useState('')
  const [userId, setUserId] = useState('')
  const [actionQ, setActionQ] = useState('')
  const [resourceTypeQ, setResourceTypeQ] = useState('')
  const [sortDesc, setSortDesc] = useState(true)
  const [applied, setApplied] = useState({
    from: '',
    to: '',
    user: '',
    action: '',
    resourceType: '',
  })
  const [users, setUsers] = useState<UserOption[]>([])

  const query = useMemo(() => {
    const p = new URLSearchParams()
    p.set('limit', String(limit))
    p.set('offset', String(offset))
    p.set('sort', sortDesc ? 'desc' : 'asc')
    if (applied.from) p.set('from', applied.from)
    if (applied.to) p.set('to', applied.to)
    if (applied.user) p.set('user_id', applied.user)
    if (applied.action) p.set('action', applied.action)
    if (applied.resourceType) p.set('resource_type', applied.resourceType)
    return p.toString()
  }, [offset, applied, sortDesc])

  const loadUsers = useCallback(async () => {
    try {
      const res = await dashboardFetch('/v1/users')
      if (!res.ok) return
      const data = (await res.json()) as UserOption[]
      setUsers(Array.isArray(data) ? data : [])
    } catch {
      setUsers([])
    }
  }, [])

  const load = useCallback(async () => {
    setErr(null)
    setLoading(true)
    try {
      const res = await dashboardFetch(`/v1/audit-logs?${query}`)
      if (!res.ok) {
        const j = (await res.json().catch(() => null)) as { error?: string } | null
        throw new Error(j?.error ?? res.statusText)
      }
      const data = (await res.json()) as AuditListResponse
      setRows(Array.isArray(data.items) ? data.items : [])
      setTotal(typeof data.total === 'number' ? data.total : 0)
    } catch (e) {
      setErr(e instanceof Error ? e.message : 'Failed to load audit logs')
      setRows([])
      setTotal(0)
    } finally {
      setLoading(false)
    }
  }, [query])

  useEffect(() => {
    if (!isAdmin) return
    void loadUsers()
  }, [isAdmin, loadUsers])

  useEffect(() => {
    if (!isAdmin) return
    void load()
  }, [isAdmin, load])

  function applyFilters(e: React.FormEvent) {
    e.preventDefault()
    setApplied({
      from: fromDate,
      to: toDate,
      user: userId,
      action: actionQ.trim(),
      resourceType: resourceTypeQ.trim(),
    })
    setOffset(0)
  }

  if (!isAdmin) {
    return <Navigate to="/" replace />
  }

  const canPrev = offset > 0
  const canNext = offset + limit < total

  return (
    <div className="flex min-h-0 flex-1 flex-col p-4 text-slate-900 dark:text-slate-100">
      <div className="mb-3 flex shrink-0 items-start gap-2">
        <ClipboardList className="mt-0.5 size-4 text-slate-400" />
        <div>
          <h1 className="text-sm font-semibold tracking-tight">System audit logs</h1>
          <p className="text-[10px] text-slate-500">
            Admin-only. Includes REST mutations, sign-ins, device commands, assignments, and dashboard WebSocket C2 dispatches.
          </p>
        </div>
      </div>

      <form
        className="mb-3 grid shrink-0 gap-2 rounded border border-slate-200 bg-slate-100/80 dark:border-slate-800 dark:bg-slate-900/40 p-2.5 sm:grid-cols-2 lg:grid-cols-6"
        onSubmit={applyFilters}
      >
        <label className="flex flex-col gap-0.5 text-[10px] font-medium text-slate-500">
          From (UTC date)
          <input
            type="date"
            className="rounded border border-slate-300 bg-white dark:border-slate-700 dark:bg-slate-950 px-1.5 py-1 text-[11px]"
            value={fromDate}
            onChange={(e) => setFromDate(e.target.value)}
          />
        </label>
        <label className="flex flex-col gap-0.5 text-[10px] font-medium text-slate-500">
          To (UTC date)
          <input
            type="date"
            className="rounded border border-slate-300 bg-white dark:border-slate-700 dark:bg-slate-950 px-1.5 py-1 text-[11px]"
            value={toDate}
            onChange={(e) => setToDate(e.target.value)}
          />
        </label>
        <label className="flex flex-col gap-0.5 text-[10px] font-medium text-slate-500">
          User
          <select
            className="rounded border border-slate-300 bg-white dark:border-slate-700 dark:bg-slate-950 px-1.5 py-1 text-[11px]"
            value={userId}
            onChange={(e) => setUserId(e.target.value)}
          >
            <option value="">All users</option>
            {users.map((u) => (
              <option key={u.id} value={u.id}>
                {u.username}
              </option>
            ))}
          </select>
        </label>
        <label className="flex flex-col gap-0.5 text-[10px] font-medium text-slate-500">
          Action contains
          <input
            className="rounded border border-slate-300 bg-white dark:border-slate-700 dark:bg-slate-950 px-1.5 py-1 font-mono text-[11px]"
            placeholder="e.g. command_executed"
            value={actionQ}
            onChange={(e) => setActionQ(e.target.value)}
          />
        </label>
        <label className="flex flex-col gap-0.5 text-[10px] font-medium text-slate-500">
          Resource type
          <input
            className="rounded border border-slate-300 bg-white dark:border-slate-700 dark:bg-slate-950 px-1.5 py-1 font-mono text-[11px]"
            placeholder="device, user, …"
            value={resourceTypeQ}
            onChange={(e) => setResourceTypeQ(e.target.value)}
          />
        </label>
        <div className="flex items-end gap-1.5">
          <button
            type="submit"
            className="rounded bg-slate-800 px-2 py-1 text-[11px] font-medium text-white hover:bg-slate-700 dark:bg-slate-100 dark:text-slate-900 dark:hover:bg-white"
          >
            Apply
          </button>
          <button
            type="button"
            className="rounded border border-slate-300 dark:border-slate-700 px-2 py-1 text-[11px] text-slate-700 hover:bg-slate-200 dark:text-slate-300 dark:hover:bg-slate-200 dark:hover:bg-slate-800"
            onClick={() => {
              setFromDate('')
              setToDate('')
              setUserId('')
              setActionQ('')
              setResourceTypeQ('')
              setApplied({ from: '', to: '', user: '', action: '', resourceType: '' })
              setOffset(0)
            }}
          >
            Clear
          </button>
        </div>
      </form>

      {err ? (
        <div className="mb-2 shrink-0 rounded border border-rose-900/60 bg-rose-950/30 px-2 py-1.5 text-[11px] text-rose-800 dark:text-rose-200">
          {err}
        </div>
      ) : null}

      <div className="mb-1 flex shrink-0 items-center justify-between text-[10px] text-slate-500">
        <span>
          {loading ? 'Loading…' : `${total} event(s)`}
          {total > 0 ? ` · showing ${offset + 1}–${Math.min(offset + limit, total)}` : null}
        </span>
        <div className="flex gap-1">
          <button
            type="button"
            disabled={!canPrev || loading}
            className="rounded border border-slate-300 dark:border-slate-700 px-2 py-0.5 text-[10px] text-slate-700 hover:bg-slate-200 dark:text-slate-300 dark:hover:bg-slate-800 disabled:opacity-40"
            onClick={() => setOffset((o) => Math.max(0, o - limit))}
          >
            Previous
          </button>
          <button
            type="button"
            disabled={!canNext || loading}
            className="rounded border border-slate-300 dark:border-slate-700 px-2 py-0.5 text-[10px] text-slate-700 hover:bg-slate-200 dark:text-slate-300 dark:hover:bg-slate-800 disabled:opacity-40"
            onClick={() => setOffset((o) => o + limit)}
          >
            Next
          </button>
        </div>
      </div>

      <div className="min-h-0 flex-1 overflow-auto rounded border border-slate-200 dark:border-slate-800">
        <table className="w-full border-collapse text-left text-[10px]">
          <thead className="sticky top-0 z-[1] bg-slate-100/95 dark:bg-slate-900/95 text-[9px] font-semibold uppercase tracking-wide text-slate-500 backdrop-blur">
            <tr>
              <th className="border-b border-slate-200 dark:border-slate-800 px-1.5 py-1">
                <button
                  type="button"
                  className="inline-flex items-center gap-0.5 hover:text-slate-800 dark:hover:text-slate-200"
                  onClick={() => {
                    setSortDesc((d) => !d)
                    setOffset(0)
                  }}
                  title="Toggle sort order"
                >
                  Timestamp {sortDesc ? '↓' : '↑'}
                </button>
              </th>
              <th className="border-b border-slate-200 dark:border-slate-800 px-1.5 py-1">User</th>
              <th className="border-b border-slate-200 dark:border-slate-800 px-1.5 py-1">Action</th>
              <th className="border-b border-slate-200 dark:border-slate-800 px-1.5 py-1">Resource</th>
              <th className="border-b border-slate-200 dark:border-slate-800 px-1.5 py-1">Target</th>
              <th className="border-b border-slate-200 dark:border-slate-800 px-1.5 py-1">IP</th>
              <th className="border-b border-slate-200 dark:border-slate-800 px-1.5 py-1">Details</th>
            </tr>
          </thead>
          <tbody className="font-mono text-slate-800 dark:text-slate-200">
            {rows.length === 0 && !loading ? (
              <tr>
                <td colSpan={7} className="px-2 py-6 text-center text-slate-500">
                  No rows
                </td>
              </tr>
            ) : null}
            {rows.map((r) => {
              const res =
                r.resource_type || r.resource_id
                  ? [r.resource_type, r.resource_id].filter(Boolean).join(' ')
                  : '—'
              return (
                <tr
                  key={r.id}
                  className="border-b border-slate-200 dark:border-slate-800/80 hover:bg-slate-100 dark:hover:bg-slate-900/50"
                >
                  <td className="whitespace-nowrap px-1.5 py-0.5 text-slate-400">{formatTs(r.timestamp)}</td>
                  <td className="max-w-[8rem] truncate px-1.5 py-0.5" title={r.username ?? r.user_id}>
                    {r.username ?? r.user_id ?? '—'}
                  </td>
                  <td className="max-w-[12rem] truncate px-1.5 py-0.5 text-emerald-200/90" title={r.action}>
                    {r.action}
                  </td>
                  <td className="max-w-[10rem] truncate px-1.5 py-0.5 text-slate-500" title={res}>
                    {res}
                  </td>
                  <td className="max-w-[8rem] truncate px-1.5 py-0.5 text-slate-400" title={r.target_human_id ?? r.target_asset_id}>
                    {r.target_human_id ?? r.target_asset_id ?? '—'}
                  </td>
                  <td className="max-w-[6rem] truncate px-1.5 py-0.5 text-slate-500" title={r.ip_address}>
                    {r.ip_address ?? '—'}
                  </td>
                  <td className="max-w-[20rem] truncate px-1.5 py-0.5 text-slate-500" title={detailsPreview(r.details)}>
                    {detailsPreview(r.details)}
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
    </div>
  )
}
