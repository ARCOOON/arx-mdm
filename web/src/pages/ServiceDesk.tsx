import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { useWebSocket } from '../hooks/useWebSocket'
import { useAuth } from '../context/AuthContext'
import { dashboardFetch } from '../lib/ticketsApi'
import { fetchUserDirectory, type UserDirectoryRow } from '../lib/deviceAssignmentApi'
import {
  ResolutionEditor,
  type ResolutionDoc,
} from '../components/ResolutionEditor'

const STATE_COLUMNS = ['new', 'in_progress', 'on_hold', 'resolved', 'closed'] as const

type IncidentListRow = {
  id: string
  incident_number: string
  short_description: string
  state: string
  priority: number
  impact: number
  urgency: number
  sla_due: string
  linked_arx_id?: string
  cmdb_ci?: string
  source_alert_fingerprint?: string
  created_at: string
  updated_at: string
  caller_id?: string
  caller_username?: string
  assigned_to?: string
  assigned_to_username?: string
}

type IncidentCMDBWire = {
  device_id: string
  human_id: string
  hostname?: string
  operational_status: string
  cost_center: string
  location: string
  last_seen?: string
  c2_connected: boolean
  cpu_usage_percent?: number
  ram_usage_percent?: number
  disk_usage_percent?: number
  latest_metrics_at?: string
  recent_c2_command_types?: string[]
  c2_capabilities: string[]
}

type IncidentDetailEnvelope = {
  incident: IncidentListRow
  work_notes: unknown
  cmdb_context?: IncidentCMDBWire
  resolutions: ResolutionDoc[]
}

type WorkNoteEntry = {
  ts?: string
  kind?: string
  text?: string
  author_type?: string
}

function priorityChipClass(p: number) {
  if (p >= 4) return 'border-rose-600/70 text-rose-200'
  if (p === 3) return 'border-amber-600/70 text-amber-200'
  if (p <= 1) return 'border-slate-500/70 text-slate-300'
  return 'border-sky-600/70 text-sky-200'
}

function parseWorkNotes(raw: unknown): WorkNoteEntry[] {
  if (raw == null) return []
  if (Array.isArray(raw)) {
    return raw.filter((x) => x && typeof x === 'object') as WorkNoteEntry[]
  }
  if (typeof raw === 'string') {
    try {
      const v = JSON.parse(raw) as unknown
      return Array.isArray(v) ? (v as WorkNoteEntry[]) : []
    } catch {
      return []
    }
  }
  return []
}


export function ServiceDeskPage() {
  const { canOperate } = useAuth()
  const { incidents: wsIncidents } = useWebSocket()
  const [rows, setRows] = useState<IncidentListRow[]>([])
  const [listErr, setListErr] = useState<string | null>(null)
  const [loadingList, setLoadingList] = useState(true)

  const [directory, setDirectory] = useState<UserDirectoryRow[]>([])
  const [directoryErr, setDirectoryErr] = useState<string | null>(null)

  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [detail, setDetail] = useState<IncidentDetailEnvelope | null>(null)
  const [detailErr, setDetailErr] = useState<string | null>(null)
  const [loadingDetail, setLoadingDetail] = useState(false)

  const [shortDescEdit, setShortDescEdit] = useState('')
  const [stateEdit, setStateEdit] = useState('')
  const [impactEdit, setImpactEdit] = useState(2)
  const [urgencyEdit, setUrgencyEdit] = useState(2)
  const [assetEdit, setAssetEdit] = useState('')
  const [assignedToEdit, setAssignedToEdit] = useState('')
  const [clearAssignee, setClearAssignee] = useState(false)
  const [slaDueLocal, setSlaDueLocal] = useState('')
  const [newWorkNote, setNewWorkNote] = useState('')

  const [appendNoteBusy, setAppendNoteBusy] = useState(false)

  const [restartSvc, setRestartSvc] = useState('')
  const [pushCfg, setPushCfg] = useState('{\n  "policy": "example"\n}\n')
  const [cmdBusy, setCmdBusy] = useState<string | null>(null)
  const [cmdMsg, setCmdMsg] = useState<string | null>(null)

  const [savingDetail, setSavingDetail] = useState(false)
  const [saveMsg, setSaveMsg] = useState<string | null>(null)
  const [deleting, setDeleting] = useState(false)

  const [showCreate, setShowCreate] = useState(false)
  const [createShort, setCreateShort] = useState('')
  const [createState, setCreateState] = useState('new')
  const [createImpact, setCreateImpact] = useState(2)
  const [createUrgency, setCreateUrgency] = useState(2)
  const [createAsset, setCreateAsset] = useState('')
  const [createAssignedTo, setCreateAssignedTo] = useState('')
  const [createSlaLocal, setCreateSlaLocal] = useState('')
  const [createInitialNote, setCreateInitialNote] = useState('')
  const [creating, setCreating] = useState(false)
  const [createErr, setCreateErr] = useState<string | null>(null)

  const loadList = useCallback(async () => {
    setListErr(null)
    setLoadingList(true)
    try {
      const res = await dashboardFetch('/v1/incidents')
      if (!res.ok) {
        const j = (await res.json().catch(() => null)) as { error?: string } | null
        throw new Error(j?.error ?? res.statusText)
      }
      const data = (await res.json()) as IncidentListRow[]
      setRows(Array.isArray(data) ? data : [])
    } catch (e) {
      setListErr(e instanceof Error ? e.message : 'Failed to load incidents')
      setRows([])
    } finally {
      setLoadingList(false)
    }
  }, [])

  const wsDigest = useMemo(
    () =>
      wsIncidents
        .map((t) =>
          [
            t.id ?? '',
            t.incident_number,
            t.state,
            String(t.priority ?? ''),
            t.sla_due,
          ].join(':'),
        )
        .join('|'),
    [wsIncidents],
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
    setCmdMsg(null)
    try {
      const res = await dashboardFetch(`/v1/incidents/${encodeURIComponent(id)}`)
      if (!res.ok) {
        const j = (await res.json().catch(() => null)) as { error?: string } | null
        throw new Error(j?.error ?? res.statusText)
      }
      const data = (await res.json()) as IncidentDetailEnvelope
      const resDocs = Array.isArray(data.resolutions)
        ? data.resolutions.map((r) => ({
            ...r,
            incident_id: id,
          }))
        : []
      const env: IncidentDetailEnvelope = {
        ...data,
        resolutions: resDocs,
      }
      setDetail(env)
      setShortDescEdit(data.incident.short_description)
      setStateEdit(data.incident.state)
      setImpactEdit(data.incident.impact ?? 2)
      setUrgencyEdit(data.incident.urgency ?? 2)
      setAssetEdit(data.incident.linked_arx_id ?? '')
      setAssignedToEdit(data.incident.assigned_to ?? '')
      setClearAssignee(false)
      setNewWorkNote('')
      try {
        const d = new Date(data.incident.sla_due)
        if (!Number.isNaN(d.getTime())) {
          const pad = (n: number) => String(n).padStart(2, '0')
          const local = `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
          setSlaDueLocal(local)
        } else {
          setSlaDueLocal('')
        }
      } catch {
        setSlaDueLocal('')
      }
    } catch (e) {
      setDetail(null)
      setDetailErr(e instanceof Error ? e.message : 'Failed to load incident')
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

  const notes = useMemo(
    () => (detail ? parseWorkNotes(detail.work_notes).slice(-200) : []),
    [detail],
  )

  const byState = useMemo(() => {
    const m = new Map<string, IncidentListRow[]>()
    for (const s of STATE_COLUMNS) {
      m.set(s, [])
    }
    for (const t of rows) {
      const st = (t.state ?? '').toLowerCase()
      const bucket = STATE_COLUMNS.includes(st as (typeof STATE_COLUMNS)[number])
        ? st
        : 'new'
      m.set(bucket, [...(m.get(bucket) ?? []), t])
    }
    return m
  }, [rows])

  async function appendWorkNoteOnly() {
    if (!selectedId) return
    const text = newWorkNote.trim()
    if (!text) return
    setAppendNoteBusy(true)
    setSaveMsg(null)
    try {
      const res = await dashboardFetch(`/v1/incidents/${encodeURIComponent(selectedId)}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ append_work_note: text }),
      })
      if (!res.ok) {
        const j = (await res.json().catch(() => null)) as { error?: string } | null
        throw new Error(j?.error ?? res.statusText)
      }
      const data = (await res.json()) as IncidentDetailEnvelope | { status?: string }
      if ('incident' in data) {
        const resDocs = Array.isArray(data.resolutions)
          ? data.resolutions.map((r) => ({
              ...r,
              incident_id: selectedId,
            }))
          : []
        setDetail({ ...data, resolutions: resDocs })
        setNewWorkNote('')
      }
      setSaveMsg('Work note added.')
      void loadList()
    } catch (e) {
      setSaveMsg(e instanceof Error ? e.message : 'Append failed')
    } finally {
      setAppendNoteBusy(false)
    }
  }

  async function saveIncidentPatch() {
    if (!selectedId) return
    setSavingDetail(true)
    setSaveMsg(null)
    try {
      const body: Record<string, string | number | boolean> = {
        short_description: shortDescEdit.trim(),
        state: stateEdit.trim(),
        impact: impactEdit,
        urgency: urgencyEdit,
        linked_asset_human_id: assetEdit.trim(),
      }
      if (slaDueLocal.trim()) {
        const d = new Date(slaDueLocal)
        if (!Number.isFinite(d.getTime())) {
          throw new Error('Invalid SLA due')
        }
        body.sla_due = d.toISOString()
      }
      if (clearAssignee) {
        body.clear_assigned_to = true
      } else if (assignedToEdit.trim()) {
        body.assigned_to_user_id = assignedToEdit.trim()
      } else if (detail?.incident.assigned_to) {
        body.assigned_to_user_id = ''
      }

      const res = await dashboardFetch(`/v1/incidents/${encodeURIComponent(selectedId)}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      })
      if (!res.ok) {
        const j = (await res.json().catch(() => null)) as { error?: string } | null
        throw new Error(j?.error ?? res.statusText)
      }
      const data = (await res.json()) as IncidentDetailEnvelope | { status?: string }
      if ('incident' in data) {
        const resDocs = Array.isArray(data.resolutions)
          ? data.resolutions.map((r) => ({
              ...r,
              incident_id: selectedId,
            }))
          : []
        setDetail({ ...data, resolutions: resDocs })
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

  async function deleteSelectedIncident() {
    if (!selectedId) return
    if (!globalThis.confirm('Delete this incident and its resolutions? This cannot be undone.')) {
      return
    }
    setDeleting(true)
    setSaveMsg(null)
    try {
      const res = await dashboardFetch(`/v1/incidents/${encodeURIComponent(selectedId)}`, {
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
    if (!selectedId) throw new Error('No incident selected')
    const res = await dashboardFetch(
      `/v1/incidents/${encodeURIComponent(selectedId)}/resolutions`,
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

  async function sendIncidentCommand(commandType: string, payload: string) {
    if (!selectedId) return
    setCmdBusy(commandType)
    setCmdMsg(null)
    try {
      const res = await dashboardFetch(
        `/v1/incidents/${encodeURIComponent(selectedId)}/commands`,
        {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ command_type: commandType, payload }),
        },
      )
      if (!res.ok) {
        const j = (await res.json().catch(() => null)) as { error?: string } | null
        throw new Error(j?.error ?? res.statusText)
      }
      setCmdMsg(`Queued ${commandType}. Terminal output journals to work notes when the agent completes the command.`)
      void loadDetail(selectedId)
      void loadList()
    } catch (e) {
      setCmdMsg(e instanceof Error ? e.message : 'Command failed')
    } finally {
      setCmdBusy(null)
    }
  }

  async function createIncident(e: React.FormEvent) {
    e.preventDefault()
    setCreateErr(null)
    const t = createShort.trim()
    if (!t) {
      setCreateErr('Short description is required.')
      return
    }
    setCreating(true)
    try {
      const body: Record<string, unknown> = {
        short_description: t,
        state: createState,
        impact: createImpact,
        urgency: createUrgency,
        linked_asset_human_id: createAsset.trim(),
      }
      if (createAssignedTo.trim()) body.assigned_to_user_id = createAssignedTo.trim()
      if (createInitialNote.trim()) body.initial_work_note = createInitialNote.trim()
      if (createSlaLocal.trim()) {
        const d = new Date(createSlaLocal)
        if (Number.isFinite(d.getTime())) body.sla_due = d.toISOString()
      }
      const res = await dashboardFetch('/v1/incidents', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      })
      if (!res.ok) {
        const j = (await res.json().catch(() => null)) as { error?: string } | null
        throw new Error(j?.error ?? res.statusText)
      }
      const data = (await res.json()) as { id: string; incident_number: string }
      setShowCreate(false)
      setCreateShort('')
      setCreateInitialNote('')
      setCreateAsset('')
      setCreateAssignedTo('')
      setCreateSlaLocal('')
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
            Service desk
          </h1>
          <p className="mt-0.5 text-[11px] text-slate-500">
            ITSM incidents (INC0000001), CMDB linkage, SLA clock, work notes, and one-click C2 from
            the detail pane.
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
            {showCreate ? 'Close form' : 'New incident'}
          </button>
        ) : null}
      </div>

      {directoryErr ? (
        <p className="mb-1 text-[11px] text-amber-400/90">{directoryErr}</p>
      ) : null}

      {showCreate && canOperate ? (
        <form
          className="mb-2 grid max-w-4xl grid-cols-2 gap-2 rounded border border-slate-200 bg-white/90 p-2.5 dark:border-slate-800 dark:bg-slate-900/50 lg:grid-cols-4"
          onSubmit={createIncident}
        >
          {createErr ? (
            <div className="col-span-full text-[11px] text-rose-400/90">{createErr}</div>
          ) : null}
          <label className="col-span-1 flex flex-col gap-0.5 text-[10px] font-medium uppercase text-slate-500">
            State
            <select
              className="rounded border border-slate-300 bg-white px-1.5 py-1 text-[12px] text-slate-900 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100"
              value={createState}
              onChange={(e) => setCreateState(e.target.value)}
            >
              {STATE_COLUMNS.map((s) => (
                <option key={s} value={s}>
                  {s}
                </option>
              ))}
            </select>
          </label>
          <label className="col-span-1 flex flex-col gap-0.5 text-[10px] font-medium uppercase text-slate-500">
            Impact (1–3)
            <input
              type="number"
              min={1}
              max={3}
              className="rounded border border-slate-300 bg-white px-1.5 py-1 font-mono text-[12px] text-slate-900 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100"
              value={createImpact}
              onChange={(e) => setCreateImpact(Number(e.target.value))}
            />
          </label>
          <label className="col-span-1 flex flex-col gap-0.5 text-[10px] font-medium uppercase text-slate-500">
            Urgency (1–3)
            <input
              type="number"
              min={1}
              max={3}
              className="rounded border border-slate-300 bg-white px-1.5 py-1 font-mono text-[12px] text-slate-900 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100"
              value={createUrgency}
              onChange={(e) => setCreateUrgency(Number(e.target.value))}
            />
          </label>
          <label className="col-span-1 flex flex-col gap-0.5 text-[10px] font-medium uppercase text-slate-500">
            SLA due (local)
            <input
              type="datetime-local"
              className="rounded border border-slate-300 bg-white px-1.5 py-1 text-[11px] text-slate-900 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100"
              value={createSlaLocal}
              onChange={(e) => setCreateSlaLocal(e.target.value)}
            />
          </label>
          <label className="col-span-1 flex flex-col gap-0.5 text-[10px] font-medium uppercase text-slate-500">
            CMDB CI (ARX human_id)
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
            Short description
            <input
              required
              className="rounded border border-slate-300 bg-white px-1.5 py-1 text-[12px] text-slate-900 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100"
              value={createShort}
              onChange={(e) => setCreateShort(e.target.value)}
            />
          </label>
          <label className="col-span-full flex flex-col gap-0.5 text-[10px] font-medium uppercase text-slate-500">
            Initial work note (optional)
            <textarea
              rows={3}
              className="rounded border border-slate-300 bg-white px-1.5 py-1 text-[12px] text-slate-900 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100"
              value={createInitialNote}
              onChange={(e) => setCreateInitialNote(e.target.value)}
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

      <div className="grid min-h-0 min-w-0 flex-1 grid-cols-1 gap-0 border border-slate-200 bg-slate-100/80 dark:border-slate-800 dark:bg-slate-900/40 xl:grid-cols-[minmax(0,1fr)_min(480px,40vw)]">
        <div className="min-h-[320px] min-w-0 overflow-x-auto border-slate-200 xl:border-r dark:border-slate-800">
          <div className="flex h-full min-w-[900px] gap-2 p-2">
            {STATE_COLUMNS.map((col) => {
              const list = byState.get(col) ?? []
              const sorted = [...list].sort((a, b) => {
                const xa = Date.parse(a.updated_at)
                const xb = Date.parse(b.updated_at)
                const va = Number.isFinite(xa) ? xa : 0
                const vb = Number.isFinite(xb) ? xb : 0
                return vb - va
              })
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
                    ) : sorted.length === 0 ? (
                      <div className="px-1 py-4 text-center text-[11px] text-slate-500">
                        No incidents
                      </div>
                    ) : (
                      sorted.map((t) => {
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
                                {t.incident_number}
                              </span>
                              <span
                                className={`shrink-0 rounded border px-1 py-0.5 text-[9px] uppercase ${priorityChipClass(
                                  t.priority,
                                )}`}
                              >
                                P{t.priority}
                              </span>
                            </div>
                            <div className="mt-0.5 line-clamp-2 font-medium text-slate-900 dark:text-slate-100">
                              {t.short_description}
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

        <aside className="flex min-h-[280px] min-w-0 flex-col border-t border-slate-200 bg-slate-100/80 dark:border-slate-800 dark:bg-slate-950/30 xl:min-h-0 xl:border-t-0">
          {!selectedId ? (
            <div className="flex flex-1 items-center justify-center px-3 py-8 text-center text-[11px] text-slate-500">
              Select a card for CMDB telemetry, resolutions, work notes, and shortcuts.
            </div>
          ) : loadingDetail ? (
            <div className="p-3 text-[11px] text-slate-500">Loading incident…</div>
          ) : detailErr ? (
            <div className="p-3 text-[11px] text-rose-400/90">{detailErr}</div>
          ) : detail ? (
            <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
              <div className="border-b border-slate-200 px-2.5 py-2 dark:border-slate-800">
                <div className="font-mono text-[13px] font-semibold text-violet-300/95">
                  {detail.incident.incident_number}
                </div>
                <div className="mt-0.5 font-mono text-[10px] text-slate-500">{detail.incident.id}</div>
                {detail.incident.caller_username ? (
                  <div className="mt-1 text-[10px] text-slate-500">
                    Caller{' '}
                    <span className="text-slate-300">{detail.incident.caller_username}</span>
                  </div>
                ) : null}
                {detail.incident.source_alert_fingerprint ? (
                  <div className="mt-1 font-mono text-[10px] text-slate-500">
                    Fingerprint {detail.incident.source_alert_fingerprint}
                  </div>
                ) : null}
              </div>
              <div className="min-h-0 flex-1 space-y-2 overflow-y-auto px-2.5 py-2">
                {detail.cmdb_context ? (
                  <div className="rounded border border-slate-200 bg-white/80 px-2 py-2 text-[11px] dark:border-slate-800 dark:bg-slate-900/60">
                    <div className="mb-1 text-[10px] font-semibold uppercase text-slate-500">
                      CMDB / telemetry
                    </div>
                    <div className="grid grid-cols-2 gap-2 text-[11px]">
                      <div>
                        <div className="text-[10px] uppercase text-slate-500">Asset</div>
                        <Link
                          to={`/assets/${encodeURIComponent(detail.cmdb_context.human_id)}`}
                          className="font-mono text-violet-400 hover:text-violet-300"
                        >
                          {detail.cmdb_context.human_id}
                        </Link>
                      </div>
                      <div>
                        <div className="text-[10px] uppercase text-slate-500">C2</div>
                        {detail.cmdb_context.c2_connected ? (
                          <span className="text-emerald-400">online</span>
                        ) : (
                          <span className="text-rose-300">offline</span>
                        )}
                      </div>
                      <div className="col-span-2 text-[10px] text-slate-500">
                        Ops {detail.cmdb_context.operational_status} · {detail.cmdb_context.location}{' '}
                        · CC {detail.cmdb_context.cost_center}
                      </div>
                      <div className="col-span-2 text-[10px] text-slate-500">
                        CPU {detail.cmdb_context.cpu_usage_percent ?? '—'}% · RAM{' '}
                        {detail.cmdb_context.ram_usage_percent ?? '—'}% · Disk{' '}
                        {detail.cmdb_context.disk_usage_percent ?? '—'}%
                      </div>
                      {detail.cmdb_context.recent_c2_command_types?.length ? (
                        <div className="col-span-2 truncate font-mono text-[10px] text-slate-500">
                          Recent C2: {detail.cmdb_context.recent_c2_command_types.join(', ')}
                        </div>
                      ) : null}
                    </div>
                  </div>
                ) : null}

                <div className="rounded border border-slate-200 bg-white/80 px-2 py-2 dark:border-slate-800 dark:bg-slate-900/60">
                  <div className="mb-1 text-[10px] font-semibold uppercase text-slate-500">
                    Work notes
                  </div>
                  <div className="max-h-[220px] space-y-1.5 overflow-y-auto text-[11px]">
                    {notes.length === 0 ? (
                      <p className="text-slate-500">No work notes recorded.</p>
                    ) : (
                      notes.map((n, idx) => (
                        <div
                          key={`${n.ts ?? ''}:${idx}`}
                          className="rounded border border-slate-100 bg-slate-50 px-2 py-1 dark:border-slate-800 dark:bg-slate-950/50"
                        >
                          <div className="font-mono text-[10px] text-slate-500">
                            {n.ts ?? '—'} · {n.kind ?? 'note'} · {n.author_type ?? '—'}
                          </div>
                          <div className="mt-1 whitespace-pre-wrap text-slate-200">{n.text ?? ''}</div>
                        </div>
                      ))
                    )}
                  </div>
                  {canOperate ? (
                    <div className="mt-2 border-t border-slate-800 pt-2">
                      <textarea
                        rows={3}
                        className="mb-1.5 w-full rounded border border-slate-300 bg-white px-1.5 py-1 text-[12px] text-slate-900 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100"
                        placeholder="Append internal work note"
                        value={newWorkNote}
                        onChange={(e) => setNewWorkNote(e.target.value)}
                        disabled={appendNoteBusy}
                      />
                      <button
                        type="button"
                        className="rounded bg-slate-700 px-2 py-1 text-[11px] text-white hover:bg-slate-600 disabled:opacity-40"
                        disabled={appendNoteBusy}
                        onClick={() => void appendWorkNoteOnly()}
                      >
                        {appendNoteBusy ? 'Saving…' : 'Add work note'}
                      </button>
                    </div>
                  ) : null}
                </div>

                {detail.cmdb_context && canOperate ? (
                  <div className="rounded border border-slate-200 bg-white/80 px-2 py-2 dark:border-slate-800 dark:bg-slate-900/60">
                    <div className="mb-1 text-[10px] font-semibold uppercase text-slate-500">
                      Incident C2
                    </div>
                    {cmdMsg ? <p className="mb-1 text-[11px] text-slate-400">{cmdMsg}</p> : null}
                    <div className="flex flex-wrap gap-2">
                      <button
                        type="button"
                        disabled={cmdBusy !== null}
                        className="rounded bg-slate-800 px-2 py-1 text-[11px] text-white hover:bg-slate-700 disabled:opacity-40"
                        onClick={() => void sendIncidentCommand('ping', '')}
                      >
                        Ping
                      </button>
                      <button
                        type="button"
                        disabled={cmdBusy !== null}
                        className="rounded bg-rose-900/70 px-2 py-1 text-[11px] text-rose-100 hover:bg-rose-800 disabled:opacity-40"
                        onClick={() =>
                          globalThis.confirm('Send reboot command to this asset now?')
                            ? void sendIncidentCommand('reboot', '')
                            : undefined
                        }
                      >
                        Reboot
                      </button>
                    </div>
                    <label className="mt-2 block text-[10px] font-semibold uppercase text-slate-500">
                      Restart service
                      <input
                        className="mt-0.5 w-full rounded border border-slate-300 bg-white px-1.5 py-1 font-mono text-[11px] dark:border-slate-700 dark:bg-slate-950"
                        placeholder="Service name (nssm/service or systemd)"
                        value={restartSvc}
                        onChange={(e) => setRestartSvc(e.target.value)}
                        disabled={cmdBusy !== null}
                      />
                    </label>
                    <button
                      type="button"
                      disabled={cmdBusy !== null || !restartSvc.trim()}
                      className="mt-1 rounded bg-amber-900/70 px-2 py-1 text-[11px] text-amber-100 hover:bg-amber-800 disabled:opacity-40"
                      onClick={() => void sendIncidentCommand('restart_service', restartSvc.trim())}
                    >
                      Dispatch restart_service
                    </button>
                    <label className="mt-2 block text-[10px] font-semibold uppercase text-slate-500">
                      push_config JSON
                      <textarea
                        rows={4}
                        className="mt-0.5 w-full rounded border border-slate-300 bg-white px-1.5 py-1 font-mono text-[11px] dark:border-slate-700 dark:bg-slate-950"
                        value={pushCfg}
                        onChange={(e) => setPushCfg(e.target.value)}
                        disabled={cmdBusy !== null}
                      />
                    </label>
                    <button
                      type="button"
                      disabled={cmdBusy !== null}
                      className="mt-1 rounded bg-emerald-900/70 px-2 py-1 text-[11px] text-emerald-100 hover:bg-emerald-800 disabled:opacity-40"
                      onClick={() => void sendIncidentCommand('push_config', pushCfg.trim())}
                    >
                      Dispatch push_config
                    </button>
                  </div>
                ) : null}

                {canOperate ? (
                  <>
                    <label className="block text-[10px] font-semibold uppercase text-slate-500">
                      Short description
                      <textarea
                        rows={4}
                        className="mt-0.5 w-full rounded border border-slate-300 bg-white px-1.5 py-1 text-[12px] text-slate-900 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100"
                        value={shortDescEdit}
                        onChange={(e) => setShortDescEdit(e.target.value)}
                      />
                    </label>
                    <div className="grid grid-cols-2 gap-2">
                      <label className="block text-[10px] font-semibold uppercase text-slate-500">
                        State
                        <select
                          className="mt-0.5 w-full rounded border border-slate-300 bg-white px-1.5 py-1 text-[12px] text-slate-900 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100"
                          value={stateEdit}
                          onChange={(e) => setStateEdit(e.target.value)}
                        >
                          {STATE_COLUMNS.map((s) => (
                            <option key={s} value={s}>
                              {s}
                            </option>
                          ))}
                        </select>
                      </label>
                      <label className="block text-[10px] font-semibold uppercase text-slate-500">
                        Priority (computed)
                        <div className="mt-2 font-mono text-[12px] text-slate-300">
                          P{detail.incident.priority} (impact {impactEdit} / urgency{' '}
                          {urgencyEdit})
                        </div>
                      </label>
                    </div>
                    <div className="grid grid-cols-2 gap-2">
                      <label className="block text-[10px] font-semibold uppercase text-slate-500">
                        Impact (1–3)
                        <input
                          type="number"
                          min={1}
                          max={3}
                          className="mt-0.5 w-full rounded border border-slate-300 bg-white px-1.5 py-1 font-mono text-[12px] dark:border-slate-700 dark:bg-slate-950"
                          value={impactEdit}
                          onChange={(e) => setImpactEdit(Number(e.target.value))}
                        />
                      </label>
                      <label className="block text-[10px] font-semibold uppercase text-slate-500">
                        Urgency (1–3)
                        <input
                          type="number"
                          min={1}
                          max={3}
                          className="mt-0.5 w-full rounded border border-slate-300 bg-white px-1.5 py-1 font-mono text-[12px] dark:border-slate-700 dark:bg-slate-950"
                          value={urgencyEdit}
                          onChange={(e) => setUrgencyEdit(Number(e.target.value))}
                        />
                      </label>
                    </div>
                    <label className="block text-[10px] font-semibold uppercase text-slate-500">
                      SLA due (local)
                      <input
                        type="datetime-local"
                        className="mt-0.5 w-full rounded border border-slate-300 bg-white px-1.5 py-1 text-[11px] dark:border-slate-700 dark:bg-slate-950"
                        value={slaDueLocal}
                        onChange={(e) => setSlaDueLocal(e.target.value)}
                      />
                    </label>
                    <label className="block text-[10px] font-semibold uppercase text-slate-500">
                      CMDB CI (ARX human_id)
                      <input
                        className="mt-0.5 w-full rounded border border-slate-300 bg-white px-1.5 py-1 font-mono text-[11px] dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100"
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
                        onClick={() => void saveIncidentPatch()}
                      >
                        {savingDetail ? 'Saving…' : 'Save incident'}
                      </button>
                      <button
                        type="button"
                        className="rounded border border-rose-800/70 bg-rose-950/30 px-2 py-1 text-[11px] text-rose-200 hover:bg-rose-950/50 disabled:opacity-40"
                        disabled={deleting}
                        onClick={() => void deleteSelectedIncident()}
                      >
                        {deleting ? 'Deleting…' : 'Delete incident'}
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
                        Summary
                      </div>
                      <div className="text-slate-100">{detail.incident.short_description}</div>
                    </div>
                    <div className="grid grid-cols-2 gap-2 text-[11px]">
                      <div>
                        <div className="text-[10px] uppercase text-slate-500">State</div>
                        {detail.incident.state}
                      </div>
                      <div>
                        <div className="text-[10px] uppercase text-slate-500">Priority</div>
                        P{detail.incident.priority}
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
