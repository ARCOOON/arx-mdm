import { useCallback, useEffect, useMemo, useState } from 'react'
import { useWebSocket } from '../hooks/useWebSocket'
import { dashboardFetch } from '../lib/ticketsApi'
import {
  ResolutionEditor,
  type ResolutionDoc,
} from '../components/ResolutionEditor'

type TicketListRow = {
  id: string
  ticket_ref: string
  title: string
  status: string
  priority: string
  linked_arx_id?: string
  asset_id?: string
  created_at: string
  updated_at: string
}

type TicketDetail = {
  ticket: TicketListRow
  resolutions: ResolutionDoc[]
}

const KINDS = ['INC', 'REQ', 'CHG', 'PRJ'] as const

export function TicketsPage() {
  const { tickets: wsTickets } = useWebSocket()
  const [rows, setRows] = useState<TicketListRow[]>([])
  const [listErr, setListErr] = useState<string | null>(null)
  const [loadingList, setLoadingList] = useState(true)

  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [detail, setDetail] = useState<TicketDetail | null>(null)
  const [detailErr, setDetailErr] = useState<string | null>(null)
  const [loadingDetail, setLoadingDetail] = useState(false)

  const [titleEdit, setTitleEdit] = useState('')
  const [statusEdit, setStatusEdit] = useState('')
  const [priorityEdit, setPriorityEdit] = useState('')
  const [assetEdit, setAssetEdit] = useState('')
  const [savingDetail, setSavingDetail] = useState(false)
  const [saveMsg, setSaveMsg] = useState<string | null>(null)

  const [showCreate, setShowCreate] = useState(false)
  const [createKind, setCreateKind] = useState<(typeof KINDS)[number]>('INC')
  const [createTitle, setCreateTitle] = useState('')
  const [createStatus, setCreateStatus] = useState('open')
  const [createPriority, setCreatePriority] = useState('normal')
  const [createAsset, setCreateAsset] = useState('')
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
      setStatusEdit(data.ticket.status)
      setPriorityEdit(data.ticket.priority)
      setAssetEdit(data.ticket.linked_arx_id ?? '')
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

  async function saveTicketPatch() {
    if (!selectedId) {
      return
    }
    setSavingDetail(true)
    setSaveMsg(null)
    try {
      const body: Record<string, string> = {
        title: titleEdit.trim(),
        status: statusEdit.trim(),
        priority: priorityEdit.trim(),
        linked_asset_human_id: assetEdit.trim(),
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
        setStatusEdit(data.ticket.status)
        setPriorityEdit(data.ticket.priority)
        setAssetEdit(data.ticket.linked_arx_id ?? '')
      }
      setSaveMsg('Saved.')
      void loadList()
    } catch (e) {
      setSaveMsg(e instanceof Error ? e.message : 'Save failed')
    } finally {
      setSavingDetail(false)
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
          status: createStatus,
          priority: createPriority,
          linked_asset_human_id: createAsset.trim(),
        }),
      })
      if (!res.ok) {
        const j = (await res.json().catch(() => null)) as { error?: string } | null
        throw new Error(j?.error ?? res.statusText)
      }
      const data = (await res.json()) as { id: string; ticket_ref: string }
      setShowCreate(false)
      setCreateTitle('')
      setCreateAsset('')
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
          <h1 className="text-lg font-semibold tracking-tight text-slate-900 dark:text-slate-100">Tickets</h1>
          <p className="mt-0.5 text-[11px] text-slate-500">
            Control plane records (INC-, REQ-, CHG-, PRJ-). Dense table + detail pane.
          </p>
        </div>
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
      </div>

      {showCreate ? (
        <form
          className="mb-2 grid max-w-3xl grid-cols-2 gap-2 rounded border border-slate-200 bg-white/90 dark:border-slate-800 dark:bg-slate-900/50 p-2.5 lg:grid-cols-4"
          onSubmit={createTicket}
        >
          {createErr ? (
            <div className="col-span-full text-[11px] text-rose-400/90">{createErr}</div>
          ) : null}
          <label className="col-span-1 flex flex-col gap-0.5 text-[10px] font-medium uppercase text-slate-500">
            Kind
            <select
              className="rounded border border-slate-300 bg-white dark:border-slate-700 dark:bg-slate-950 px-1.5 py-1 text-[12px] text-slate-900 dark:text-slate-100"
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
              className="rounded border border-slate-300 bg-white dark:border-slate-700 dark:bg-slate-950 px-1.5 py-1 text-[12px] text-slate-900 dark:text-slate-100"
              value={createStatus}
              onChange={(e) => setCreateStatus(e.target.value)}
            >
              {['open', 'in_progress', 'pending', 'resolved', 'closed'].map((s) => (
                <option key={s} value={s}>
                  {s}
                </option>
              ))}
            </select>
          </label>
          <label className="col-span-1 flex flex-col gap-0.5 text-[10px] font-medium uppercase text-slate-500">
            Priority
            <select
              className="rounded border border-slate-300 bg-white dark:border-slate-700 dark:bg-slate-950 px-1.5 py-1 text-[12px] text-slate-900 dark:text-slate-100"
              value={createPriority}
              onChange={(e) => setCreatePriority(e.target.value)}
            >
              {['critical', 'high', 'normal', 'low'].map((p) => (
                <option key={p} value={p}>
                  {p}
                </option>
              ))}
            </select>
          </label>
          <label className="col-span-1 flex flex-col gap-0.5 text-[10px] font-medium uppercase text-slate-500">
            Linked ARX ID
            <input
              className="rounded border border-slate-300 bg-white dark:border-slate-700 dark:bg-slate-950 px-1.5 py-1 font-mono text-[11px] text-slate-900 dark:text-slate-100"
              placeholder="arx-c-1"
              value={createAsset}
              onChange={(e) => setCreateAsset(e.target.value)}
            />
          </label>
          <label className="col-span-full flex flex-col gap-0.5 text-[10px] font-medium uppercase text-slate-500 lg:col-span-2">
            Title
            <input
              required
              className="rounded border border-slate-300 bg-white dark:border-slate-700 dark:bg-slate-950 px-1.5 py-1 text-[12px] text-slate-900 dark:text-slate-100"
              value={createTitle}
              onChange={(e) => setCreateTitle(e.target.value)}
            />
          </label>
          <div className="col-span-full flex items-center gap-2 lg:col-span-2">
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

      {listErr ? (
        <p className="mb-2 text-[11px] text-rose-400/90">{listErr}</p>
      ) : null}

      <div className="grid min-h-0 min-w-0 flex-1 grid-cols-1 gap-0 border border-slate-200 bg-slate-100/80 dark:border-slate-800 dark:bg-slate-900/40 lg:grid-cols-[minmax(0,1fr)_min(440px,38vw)]">
        <div className="min-w-0 overflow-auto border-slate-200 lg:border-r dark:border-slate-800">
          <table className="w-full min-w-[720px] border-collapse text-left text-[12px]">
            <thead className="sticky top-0 z-[1] border-b border-slate-200 bg-slate-100/95 text-[11px] dark:border-slate-800 dark:bg-slate-900/95 font-semibold uppercase tracking-wide text-slate-500">
              <tr>
                <th className="whitespace-nowrap px-1.5 py-1">ID</th>
                <th className="min-w-[200px] px-1.5 py-1">Title</th>
                <th className="px-1.5 py-1">Status</th>
                <th className="px-1.5 py-1">Priority</th>
                <th className="px-1.5 py-1">Linked ARX ID</th>
              </tr>
            </thead>
            <tbody className="text-slate-200">
              {loadingList && rows.length === 0 ? (
                <tr>
                  <td colSpan={5} className="px-2 py-6 text-center text-[11px] text-slate-500">
                    Loading…
                  </td>
                </tr>
              ) : rows.length === 0 ? (
                <tr>
                  <td colSpan={5} className="px-2 py-8 text-center text-[11px] text-slate-500">
                    No tickets. Use New ticket or the REST API.
                  </td>
                </tr>
              ) : (
                rows.map((t) => {
                  const sel = t.id === selectedId
                  return (
                    <tr
                      key={t.id}
                      className={`cursor-pointer border-b border-slate-200 dark:border-slate-800/80 hover:bg-slate-800/35 ${
                        sel ? 'bg-violet-950/25' : ''
                      }`}
                      onClick={() => setSelectedId(t.id)}
                    >
                      <td className="whitespace-nowrap px-1.5 py-0.5 font-mono text-[11px] text-violet-300/95">
                        {t.ticket_ref}
                      </td>
                      <td className="max-w-[1px] truncate px-1.5 py-0.5 font-medium text-slate-900 dark:text-slate-100">
                        {t.title}
                      </td>
                      <td className="whitespace-nowrap px-1.5 py-0.5 text-slate-400">{t.status}</td>
                      <td className="whitespace-nowrap px-1.5 py-0.5 text-slate-400">{t.priority}</td>
                      <td className="whitespace-nowrap px-1.5 py-0.5 font-mono text-[11px] text-sky-300/90">
                        {t.linked_arx_id ?? '—'}
                      </td>
                    </tr>
                  )
                })
              )}
            </tbody>
          </table>
        </div>

        <aside className="flex min-h-[280px] min-w-0 flex-col border-t border-slate-200 bg-slate-100/80 dark:border-slate-800 dark:bg-slate-950/30 lg:min-h-0 lg:border-t-0">
          {!selectedId ? (
            <div className="flex flex-1 items-center justify-center px-3 py-8 text-center text-[11px] text-slate-500">
              Select a ticket to view details, edit fields, and manage resolutions.
            </div>
          ) : loadingDetail ? (
            <div className="p-3 text-[11px] text-slate-500">Loading ticket…</div>
          ) : detailErr ? (
            <div className="p-3 text-[11px] text-rose-400/90">{detailErr}</div>
          ) : detail ? (
            <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
              <div className="border-b border-slate-200 dark:border-slate-800 px-2.5 py-2">
                <div className="font-mono text-[13px] font-semibold text-violet-300/95">
                  {detail.ticket.ticket_ref}
                </div>
                <div className="mt-0.5 font-mono text-[10px] text-slate-500">{detail.ticket.id}</div>
              </div>
              <div className="min-h-0 flex-1 space-y-2 overflow-y-auto px-2.5 py-2">
                <label className="block text-[10px] font-semibold uppercase text-slate-500">
                  Title
                  <input
                    className="mt-0.5 w-full rounded border border-slate-300 bg-white dark:border-slate-700 dark:bg-slate-950 px-1.5 py-1 text-[12px] text-slate-900 dark:text-slate-100"
                    value={titleEdit}
                    onChange={(e) => setTitleEdit(e.target.value)}
                  />
                </label>
                <div className="grid grid-cols-2 gap-2">
                  <label className="block text-[10px] font-semibold uppercase text-slate-500">
                    Status
                    <select
                      className="mt-0.5 w-full rounded border border-slate-300 bg-white dark:border-slate-700 dark:bg-slate-950 px-1.5 py-1 text-[12px] text-slate-900 dark:text-slate-100"
                      value={statusEdit}
                      onChange={(e) => setStatusEdit(e.target.value)}
                    >
                      {['open', 'in_progress', 'pending', 'resolved', 'closed'].map((s) => (
                        <option key={s} value={s}>
                          {s}
                        </option>
                      ))}
                    </select>
                  </label>
                  <label className="block text-[10px] font-semibold uppercase text-slate-500">
                    Priority
                    <select
                      className="mt-0.5 w-full rounded border border-slate-300 bg-white dark:border-slate-700 dark:bg-slate-950 px-1.5 py-1 text-[12px] text-slate-900 dark:text-slate-100"
                      value={priorityEdit}
                      onChange={(e) => setPriorityEdit(e.target.value)}
                    >
                      {['critical', 'high', 'normal', 'low'].map((p) => (
                        <option key={p} value={p}>
                          {p}
                        </option>
                      ))}
                    </select>
                  </label>
                </div>
                <label className="block text-[10px] font-semibold uppercase text-slate-500">
                  Linked ARX ID
                  <input
                    className="mt-0.5 w-full rounded border border-slate-300 bg-white dark:border-slate-700 dark:bg-slate-950 px-1.5 py-1 font-mono text-[11px] text-slate-900 dark:text-slate-100"
                    placeholder="Clear to unlink"
                    value={assetEdit}
                    onChange={(e) => setAssetEdit(e.target.value)}
                  />
                </label>
                <div className="flex items-center gap-2">
                  <button
                    type="button"
                    className="rounded bg-slate-800 px-2 py-1 text-[11px] text-white hover:bg-slate-700 disabled:opacity-40 dark:bg-slate-700 dark:hover:bg-slate-600"
                    disabled={savingDetail}
                    onClick={() => void saveTicketPatch()}
                  >
                    {savingDetail ? 'Saving…' : 'Save changes'}
                  </button>
                  {saveMsg ? (
                    <span className="text-[11px] text-slate-400">{saveMsg}</span>
                  ) : null}
                </div>
                <ResolutionEditor
                  resolutions={detail.resolutions}
                  onSubmit={postResolution}
                />
              </div>
            </div>
          ) : null}
        </aside>
      </div>
    </div>
  )
}
