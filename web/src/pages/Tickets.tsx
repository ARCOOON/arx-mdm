import { useCallback, useEffect, useMemo, useState } from 'react'
import { useWebSocket } from '../hooks/useWebSocket'
import { useAuth } from '../context/AuthContext'
import { dashboardFetch } from '../lib/ticketsApi'
import { fetchUserDirectory, type UserDirectoryRow } from '../lib/deviceAssignmentApi'
import {
  ResolutionEditor,
  type ResolutionDoc,
} from '../components/ResolutionEditor'

const STATUS_COLUMNS = ['open', 'in_progress', 'resolved', 'closed'] as const

type TicketListRow = {
  id: string
  ticket_ref: string
  title: string
  description: string
  status: string
  priority: string
  linked_arx_id?: string
  device_id?: string
  created_at: string
  updated_at: string
  created_by?: string
  created_by_username?: string
  assigned_to?: string
  assigned_to_username?: string
}

type TicketDetail = {
  ticket: TicketListRow
  resolutions: ResolutionDoc[]
}

const KINDS = ['INC', 'REQ', 'CHG', 'PRJ'] as const

function priorityChipClass(p: string) {
  const x = (p ?? '').toLowerCase()
  if (x === 'critical') return 'border-rose-600/70 text-rose-200'
  if (x === 'high') return 'border-amber-600/70 text-amber-200'
  if (x === 'low') return 'border-slate-500/70 text-slate-300'
  return 'border-sky-600/70 text-sky-200'
}

export function TicketsPage() {
  const { canOperate } = useAuth()
  const { tickets: wsTickets } = useWebSocket()
  const [rows, setRows] = useState<TicketListRow[]>([])
  const [listErr, setListErr] = useState<string | null>(null)
  const [loadingList, setLoadingList] = useState(true)

  const [directory, setDirectory] = useState<UserDirectoryRow[]>([])
  const [directoryErr, setDirectoryErr] = useState<string | null>(null)

  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [detail, setDetail] = useState<TicketDetail | null>(null)
  const [detailErr, setDetailErr] = useState<string | null>(null)
  const [loadingDetail, setLoadingDetail] = useState(false)

  const [titleEdit, setTitleEdit] = useState('')
  const [descriptionEdit, setDescriptionEdit] = useState('')
  const [statusEdit, setStatusEdit] = useState('')
  const [priorityEdit, setPriorityEdit] = useState('')
  const [assetEdit, setAssetEdit] = useState('')
  const [assignedToEdit, setAssignedToEdit] = useState('')
  const [clearAssignee, setClearAssignee] = useState(false)

  const [savingDetail, setSavingDetail] = useState(false)
  const [saveMsg, setSaveMsg] = useState<string | null>(null)
  const [deleting, setDeleting] = useState(false)

  const [showCreate, setShowCreate] = useState(false)
  const [createKind, setCreateKind] = useState<(typeof KINDS)[number]>('INC')
  const [createTitle, setCreateTitle] = useState('')
  const [createDescription, setCreateDescription] = useState('')
  const [createStatus, setCreateStatus] = useState('open')
  const [createPriority, setCreatePriority] = useState('medium')
  const [createAsset, setCreateAsset] = useState('')
  const [createAssignedTo, setCreateAssignedTo] = useState('')
  const [creating, setCreating] = useState(false)
  const [createErr, setCreateErr] = useState<string | null>(null)

  const loadList = useCallback(async () => {
    setListErr(null)
    setLoadingList(true)
    try {
      const res = await dashboardFetch('/v1/tickets')
      if (!res.ok) {
        const j = (await res.json().catch(() => null)) as { error?: string } | null
        throw new Error(j?.error ?? res.statusText)
      }
      const data = (await res.json()) as TicketListRow[]
      setRows(Array.isArray(data) ? data : [])
    } catch (e) {
      setListErr(e instanceof Error ? e.message : 'Failed to load tickets')
      setRows([])
    } finally {
      setLoadingList(false)
    }
  }, [])

  const wsDigest = useMemo(
    () =>
      wsTickets
        .map((t) => `${t.id ?? ''}:${t.ticket_ref}:${t.status}:${t.priority ?? ''}`)
        .join('|'),
    [wsTickets],
  )

  useEffect(() => {
    void loadList()
  }, [loadList, wsDigest])

  useEffect(() => {
    if (!canOperate) {
      setDirectory([])
      return
    }
    setDirectoryErr(null)
    void (async () => {
      try {
        const users = await fetchUserDirectory()
        setDirectory(users)
      } catch (e) {
        setDirectoryErr(e instanceof Error ? e.message : 'Failed to load user directory')
        setDirectory([])
      }
    })()
  }, [canOperate])

  const loadDetail = useCallback(async (id: string) => {
    setDetailErr(null)
    setLoadingDetail(true)
    setSaveMsg(null)
    try {
      const res = await dashboardFetch(`/v1/tickets/${encodeURIComponent(id)}`)
      if (!res.ok) {
        const j = (await res.json().catch(() => null)) as { error?: string } | null
        throw new Error(j?.error ?? res.statusText)
      }
      const data = (await res.json()) as TicketDetail
      setDetail(data)
      setTitleEdit(data.ticket.title)
      setDescriptionEdit(data.ticket.description ?? '')
      setStatusEdit(data.ticket.status)
      setPriorityEdit(data.ticket.priority)
      setAssetEdit(data.ticket.linked_arx_id ?? '')
      setAssignedToEdit(data.ticket.assigned_to ?? '')
      setClearAssignee(false)
    } catch (e) {
      setDetail(null)
      setDetailErr(e instanceof Error ? e.message : 'Failed to load ticket')
    } finally {
      setLoadingDetail(false)
    }
  }, [])

  useEffect(() => {
    if (!selectedId) {
      setDetail(null)
      return
    }
    void loadDetail(selectedId)
  }, [selectedId, loadDetail])

  const byStatus = useMemo(() => {
    const m = new Map<string, TicketListRow[]>()
    for (const s of STATUS_COLUMNS) {
      m.set(s, [])
    }
    for (const t of rows) {
      const st = (t.status ?? '').toLowerCase()
      const bucket = STATUS_COLUMNS.includes(st as (typeof STATUS_COLUMNS)[number]) ? st : 'open'
      m.set(bucket, [...(m.get(bucket) ?? []), t])
    }
    return m
  }, [rows])

  async function saveTicketPatch() {
    if (!selectedId) {
      return
    }
    setSavingDetail(true)
    setSaveMsg(null)
    try {
      const body: Record<string, string | boolean> = {
        title: titleEdit.trim(),
        description: descriptionEdit,
        status: statusEdit.trim(),
        priority: priorityEdit.trim(),
        linked_asset_human_id: assetEdit.trim(),
      }
      if (clearAssignee) {
        body.clear_assigned_to = true
      } else if (assignedToEdit.trim()) {
        body.assigned_to_user_id = assignedToEdit.trim()
      } else if (detail?.ticket.assigned_to) {
        body.assigned_to_user_id = ''
      }
      const res = await dashboardFetch(`/v1/tickets/${encodeURIComponent(selectedId)}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      })
      if (!res.ok) {
        const j = (await res.json().catch(() => null)) as { error?: string } | null
        throw new Error(j?.error ?? res.statusText)
      }
      const data = (await res.json()) as TicketDetail | { status?: string }
      if ('ticket' in data) {
        setDetail(data)
        setTitleEdit(data.ticket.title)
        setDescriptionEdit(data.ticket.description ?? '')
        setStatusEdit(data.ticket.status)
        setPriorityEdit(data.ticket.priority)
        setAssetEdit(data.ticket.linked_arx_id ?? '')
        setAssignedToEdit(data.ticket.assigned_to ?? '')
        setClearAssignee(false)
      }
      setSaveMsg('Saved.')
      void loadList()
    } catch (e) {
      setSaveMsg(e instanceof Error ? e.message : 'Save failed')
    } finally {
      setSavingDetail(false)
    }
  }

  async function deleteSelectedTicket() {
    if (!selectedId) {
      return
    }
    if (!globalThis.confirm('Delete this ticket and its resolutions? This cannot be undone.')) {
      return
    }
    setDeleting(true)
    setSaveMsg(null)
    try {
      const res = await dashboardFetch(`/v1/tickets/${encodeURIComponent(selectedId)}`, {
        method: 'DELETE',
      })
      if (!res.ok && res.status !== 204) {
        const j = (await res.json().catch(() => null)) as { error?: string } | null
        throw new Error(j?.error ?? res.statusText)
      }
      setSelectedId(null)
      setDetail(null)
      void loadList()
    } catch (e) {
      setSaveMsg(e instanceof Error ? e.message : 'Delete failed')
    } finally {
      setDeleting(false)
    }
  }

  async function postResolution(payload: { summary: string; markdown: string }) {
    if (!selectedId) {
      throw new Error('No ticket selected')
    }
    const res = await dashboardFetch(
      `/v1/tickets/${encodeURIComponent(selectedId)}/resolutions`,
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      },
    )
    if (!res.ok) {
      const j = (await res.json().catch(() => null)) as { error?: string } | null
      throw new Error(j?.error ?? res.statusText)
    }
    await loadDetail(selectedId)
    void loadList()
  }

  async function createTicket(e: React.FormEvent) {
    e.preventDefault()
    setCreateErr(null)
    const t = createTitle.trim()
    if (!t) {
      setCreateErr('Title is required.')
      return
    }
    setCreating(true)
    try {
      const res = await dashboardFetch('/v1/tickets', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          kind: createKind,
          title: t,
          description: createDescription,
          status: createStatus,
          priority: createPriority,
          linked_asset_human_id: createAsset.trim(),
          assigned_to_user_id: createAssignedTo.trim(),
        }),
      })
      if (!res.ok) {
        const j = (await res.json().catch(() => null)) as { error?: string } | null
        throw new Error(j?.error ?? res.statusText)
      }
      const data = (await res.json()) as { id: string; ticket_ref: string }
      setShowCreate(false)
      setCreateTitle('')
      setCreateDescription('')
      setCreateAsset('')
      setCreateAssignedTo('')
      void loadList()
      setSelectedId(data.id)
    } catch (e) {
      setCreateErr(e instanceof Error ? e.message : 'Create failed')
    } finally {
      setCreating(false)
    }
  }

  return (
    <div className="flex min-h-full min-w-0 flex-col bg-slate-50 px-4 py-3 dark:bg-slate-950 lg:px-6">
      <div className="mb-2 flex flex-wrap items-end justify-between gap-2">
        <div>
          <h1 className="text-lg font-semibold tracking-tight text-slate-900 dark:text-slate-100">
            Tickets
          </h1>
          <p className="mt-0.5 text-[11px] text-slate-500">
            ITSM board (INC-, REQ-, CHG-, PRJ-) with device linkage, ownership, and operator
            workflow.
          </p>
        </div>
        {canOperate ? (
          <button
            type="button"
            className="rounded border border-slate-400 dark:border-slate-600 bg-slate-800 px-2 py-1 text-[11px] font-medium text-slate-200 hover:bg-slate-700"
            onClick={() => {
              setShowCreate((v) => !v)
              setCreateErr(null)
            }}
          >
            {showCreate ? 'Close form' : 'New ticket'}
          </button>
        ) : null}
      </div>

      {directoryErr ? (
        <p className="mb-1 text-[11px] text-amber-400/90">{directoryErr}</p>
      ) : null}

      {showCreate && canOperate ? (
        <form
          className="mb-2 grid max-w-4xl grid-cols-2 gap-2 rounded border border-slate-200 bg-white/90 p-2.5 dark:border-slate-800 dark:bg-slate-900/50 lg:grid-cols-4"
          onSubmit={createTicket}
        >
          {createErr ? (
            <div className="col-span-full text-[11px] text-rose-400/90">{createErr}</div>
          ) : null}
          <label className="col-span-1 flex flex-col gap-0.5 text-[10px] font-medium uppercase text-slate-500">
            Kind
            <select
              className="rounded border border-slate-300 bg-white px-1.5 py-1 text-[12px] text-slate-900 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100"
              value={createKind}
              onChange={(e) => setCreateKind(e.target.value as (typeof KINDS)[number])}
            >
              {KINDS.map((k) => (
                <option key={k} value={k}>
                  {k}
                </option>
              ))}
            </select>
          </label>
          <label className="col-span-1 flex flex-col gap-0.5 text-[10px] font-medium uppercase text-slate-500">
            Status
            <select
              className="rounded border border-slate-300 bg-white px-1.5 py-1 text-[12px] text-slate-900 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100"
              value={createStatus}
              onChange={(e) => setCreateStatus(e.target.value)}
            >
              {STATUS_COLUMNS.map((s) => (
                <option key={s} value={s}>
                  {s}
                </option>
              ))}
            </select>
          </label>
          <label className="col-span-1 flex flex-col gap-0.5 text-[10px] font-medium uppercase text-slate-500">
            Priority
            <select
              className="rounded border border-slate-300 bg-white px-1.5 py-1 text-[12px] text-slate-900 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100"
              value={createPriority}
              onChange={(e) => setCreatePriority(e.target.value)}
            >
              {['critical', 'high', 'medium', 'low'].map((p) => (
                <option key={p} value={p}>
                  {p}
                </option>
              ))}
            </select>
          </label>
          <label className="col-span-1 flex flex-col gap-0.5 text-[10px] font-medium uppercase text-slate-500">
            Device ARX ID
            <input
              className="rounded border border-slate-300 bg-white px-1.5 py-1 font-mono text-[11px] text-slate-900 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100"
              placeholder="arx-c-1"
              value={createAsset}
              onChange={(e) => setCreateAsset(e.target.value)}
            />
          </label>
          <label className="col-span-1 flex flex-col gap-0.5 text-[10px] font-medium uppercase text-slate-500 lg:col-span-2">
            Assignee
            <select
              className="rounded border border-slate-300 bg-white px-1.5 py-1 text-[12px] text-slate-900 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100"
              value={createAssignedTo}
              onChange={(e) => setCreateAssignedTo(e.target.value)}
            >
              <option value="">Unassigned</option>
              {directory.map((u) => (
                <option key={u.id} value={u.id}>
                  {u.username}
                </option>
              ))}
            </select>
          </label>
          <label className="col-span-full flex flex-col gap-0.5 text-[10px] font-medium uppercase text-slate-500 lg:col-span-2">
            Title
            <input
              required
              className="rounded border border-slate-300 bg-white px-1.5 py-1 text-[12px] text-slate-900 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100"
              value={createTitle}
              onChange={(e) => setCreateTitle(e.target.value)}
            />
          </label>
          <label className="col-span-full flex flex-col gap-0.5 text-[10px] font-medium uppercase text-slate-500">
            Description
            <textarea
              rows={3}
              className="rounded border border-slate-300 bg-white px-1.5 py-1 text-[12px] text-slate-900 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100"
              value={createDescription}
              onChange={(e) => setCreateDescription(e.target.value)}
            />
          </label>
          <div className="col-span-full flex items-center gap-2">
            <button
              type="submit"
              disabled={creating}
              className="rounded bg-violet-700 px-2.5 py-1 text-[11px] font-medium text-white hover:bg-violet-600 disabled:opacity-40"
            >
              {creating ? 'Creating…' : 'Create'}
            </button>
          </div>
        </form>
      ) : null}

      {listErr ? <p className="mb-2 text-[11px] text-rose-400/90">{listErr}</p> : null}

      <div className="grid min-h-0 min-w-0 flex-1 grid-cols-1 gap-0 border border-slate-200 bg-slate-100/80 dark:border-slate-800 dark:bg-slate-900/40 lg:grid-cols-[minmax(0,1fr)_min(440px,38vw)]">
        <div className="min-h-[320px] min-w-0 overflow-x-auto border-slate-200 lg:border-r dark:border-slate-800">
          <div className="flex h-full min-w-[720px] gap-2 p-2">
            {STATUS_COLUMNS.map((col) => {
              const list = byStatus.get(col) ?? []
              return (
                <div
                  key={col}
                  className="flex min-w-0 flex-1 flex-col rounded border border-slate-200 bg-slate-50/90 dark:border-slate-800 dark:bg-slate-950/40"
                >
                  <div className="border-b border-slate-200 px-2 py-1.5 text-[10px] font-semibold uppercase tracking-wide text-slate-500 dark:border-slate-800">
                    {col.replace(/_/g, ' ')}{' '}
                    <span className="font-mono font-normal normal-case text-slate-400">
                      ({list.length})
                    </span>
                  </div>
                  <div className="flex-1 space-y-1.5 overflow-y-auto p-1.5">
                    {loadingList && rows.length === 0 ? (
                      <div className="px-1 py-4 text-center text-[11px] text-slate-500">
                        Loading…
                      </div>
                    ) : list.length === 0 ? (
                      <div className="px-1 py-4 text-center text-[11px] text-slate-500">
                        No tickets
                      </div>
                    ) : (
                      list.map((t) => {
                        const sel = t.id === selectedId
                        return (
                          <button
                            type="button"
                            key={t.id}
                            onClick={() => setSelectedId(t.id)}
                            className={`w-full rounded border px-2 py-1.5 text-left text-[11px] transition-colors ${
                              sel
                                ? 'border-violet-500/80 bg-violet-950/30'
                                : 'border-slate-200 bg-white/80 hover:bg-slate-100 dark:border-slate-700 dark:bg-slate-900/60 dark:hover:bg-slate-800/80'
                            }`}
                          >
                            <div className="flex items-start justify-between gap-2">
                              <span className="font-mono text-[10px] text-violet-300/95">
                                {t.ticket_ref}
                              </span>
                              <span
                                className={`shrink-0 rounded border px-1 py-0.5 text-[9px] uppercase ${priorityChipClass(
                                  t.priority,
                                )}`}
                              >
                                {t.priority}
                              </span>
                            </div>
                            <div className="mt-0.5 line-clamp-2 font-medium text-slate-900 dark:text-slate-100">
                              {t.title}
                            </div>
                            {t.assigned_to_username ? (
                              <div className="mt-1 truncate text-[10px] text-sky-400/90">
                                {t.assigned_to_username}
                              </div>
                            ) : null}
                            {t.linked_arx_id ? (
                              <div className="mt-0.5 truncate font-mono text-[10px] text-slate-500">
                                {t.linked_arx_id}
                              </div>
                            ) : null}
                          </button>
                        )
                      })
                    )}
                  </div>
                </div>
              )
            })}
          </div>
        </div>

        <aside className="flex min-h-[280px] min-w-0 flex-col border-t border-slate-200 bg-slate-100/80 dark:border-slate-800 dark:bg-slate-950/30 lg:min-h-0 lg:border-t-0">
          {!selectedId ? (
            <div className="flex flex-1 items-center justify-center px-3 py-8 text-center text-[11px] text-slate-500">
              Select a card to inspect the full record, update fields, and log resolutions.
            </div>
          ) : loadingDetail ? (
            <div className="p-3 text-[11px] text-slate-500">Loading ticket…</div>
          ) : detailErr ? (
            <div className="p-3 text-[11px] text-rose-400/90">{detailErr}</div>
          ) : detail ? (
            <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
              <div className="border-b border-slate-200 px-2.5 py-2 dark:border-slate-800">
                <div className="font-mono text-[13px] font-semibold text-violet-300/95">
                  {detail.ticket.ticket_ref}
                </div>
                <div className="mt-0.5 font-mono text-[10px] text-slate-500">{detail.ticket.id}</div>
                {detail.ticket.created_by_username ? (
                  <div className="mt-1 text-[10px] text-slate-500">
                    Opened by{' '}
                    <span className="text-slate-300">{detail.ticket.created_by_username}</span>
                  </div>
                ) : null}
              </div>
              <div className="min-h-0 flex-1 space-y-2 overflow-y-auto px-2.5 py-2">
                {canOperate ? (
                  <>
                    <label className="block text-[10px] font-semibold uppercase text-slate-500">
                      Title
                      <input
                        className="mt-0.5 w-full rounded border border-slate-300 bg-white px-1.5 py-1 text-[12px] text-slate-900 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100"
                        value={titleEdit}
                        onChange={(e) => setTitleEdit(e.target.value)}
                      />
                    </label>
                    <label className="block text-[10px] font-semibold uppercase text-slate-500">
                      Description
                      <textarea
                        rows={5}
                        className="mt-0.5 w-full rounded border border-slate-300 bg-white px-1.5 py-1 text-[12px] text-slate-900 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100"
                        value={descriptionEdit}
                        onChange={(e) => setDescriptionEdit(e.target.value)}
                      />
                    </label>
                    <div className="grid grid-cols-2 gap-2">
                      <label className="block text-[10px] font-semibold uppercase text-slate-500">
                        Status
                        <select
                          className="mt-0.5 w-full rounded border border-slate-300 bg-white px-1.5 py-1 text-[12px] text-slate-900 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100"
                          value={statusEdit}
                          onChange={(e) => setStatusEdit(e.target.value)}
                        >
                          {STATUS_COLUMNS.map((s) => (
                            <option key={s} value={s}>
                              {s}
                            </option>
                          ))}
                        </select>
                      </label>
                      <label className="block text-[10px] font-semibold uppercase text-slate-500">
                        Priority
                        <select
                          className="mt-0.5 w-full rounded border border-slate-300 bg-white px-1.5 py-1 text-[12px] text-slate-900 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100"
                          value={priorityEdit}
                          onChange={(e) => setPriorityEdit(e.target.value)}
                        >
                          {['critical', 'high', 'medium', 'low'].map((p) => (
                            <option key={p} value={p}>
                              {p}
                            </option>
                          ))}
                        </select>
                      </label>
                    </div>
                    <label className="block text-[10px] font-semibold uppercase text-slate-500">
                      Device ARX ID
                      <input
                        className="mt-0.5 w-full rounded border border-slate-300 bg-white px-1.5 py-1 font-mono text-[11px] text-slate-900 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100"
                        placeholder="Clear to unlink"
                        value={assetEdit}
                        onChange={(e) => setAssetEdit(e.target.value)}
                      />
                    </label>
                    <label className="block text-[10px] font-semibold uppercase text-slate-500">
                      Assignee
                      <select
                        className="mt-0.5 w-full rounded border border-slate-300 bg-white px-1.5 py-1 text-[12px] text-slate-900 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100"
                        value={assignedToEdit}
                        onChange={(e) => setAssignedToEdit(e.target.value)}
                        disabled={clearAssignee}
                      >
                        <option value="">Unassigned</option>
                        {directory.map((u) => (
                          <option key={u.id} value={u.id}>
                            {u.username}
                          </option>
                        ))}
                      </select>
                    </label>
                    <label className="flex items-center gap-2 text-[11px] text-slate-400">
                      <input
                        type="checkbox"
                        checked={clearAssignee}
                        onChange={(e) => setClearAssignee(e.target.checked)}
                      />
                      Clear assignee on save
                    </label>
                    <div className="flex flex-wrap items-center gap-2">
                      <button
                        type="button"
                        className="rounded bg-slate-800 px-2 py-1 text-[11px] text-white hover:bg-slate-700 disabled:opacity-40 dark:bg-slate-700 dark:hover:bg-slate-600"
                        disabled={savingDetail}
                        onClick={() => void saveTicketPatch()}
                      >
                        {savingDetail ? 'Saving…' : 'Save changes'}
                      </button>
                      <button
                        type="button"
                        className="rounded border border-rose-800/70 bg-rose-950/30 px-2 py-1 text-[11px] text-rose-200 hover:bg-rose-950/50 disabled:opacity-40"
                        disabled={deleting}
                        onClick={() => void deleteSelectedTicket()}
                      >
                        {deleting ? 'Deleting…' : 'Delete ticket'}
                      </button>
                      {saveMsg ? (
                        <span className="text-[11px] text-slate-400">{saveMsg}</span>
                      ) : null}
                    </div>
                  </>
                ) : (
                  <div className="space-y-2 text-[12px] text-slate-200">
                    <div>
                      <div className="text-[10px] font-semibold uppercase text-slate-500">
                        Title
                      </div>
                      <div className="text-slate-100">{detail.ticket.title}</div>
                    </div>
                    <div>
                      <div className="text-[10px] font-semibold uppercase text-slate-500">
                        Description
                      </div>
                      <div className="whitespace-pre-wrap text-slate-300">
                        {detail.ticket.description || '—'}
                      </div>
                    </div>
                    <div className="grid grid-cols-2 gap-2 text-[11px]">
                      <div>
                        <div className="text-[10px] uppercase text-slate-500">Status</div>
                        {detail.ticket.status}
                      </div>
                      <div>
                        <div className="text-[10px] uppercase text-slate-500">Priority</div>
                        {detail.ticket.priority}
                      </div>
                    </div>
                  </div>
                )}
                <ResolutionEditor
                  resolutions={detail.resolutions}
                  onSubmit={postResolution}
                  disabled={!canOperate}
                />
              </div>
            </div>
          ) : null}
        </aside>
      </div>
    </div>
  )
}
