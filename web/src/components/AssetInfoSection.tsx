import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { useAuth } from '../context/AuthContext'
import {
  fetchDeviceAssignment,
  fetchUserDirectory,
  postDeviceAssign,
  postDeviceUnassign,
  type DeviceAssignment,
  type UserDirectoryRow,
} from '../lib/deviceAssignmentApi'
import { dashboardFetch } from '../lib/ticketsApi'

type TicketListRow = {
  id: string
  ticket_ref: string
  title: string
  status: string
  priority: string
}

type AssetInfoSectionProps = {
  deviceId: string
  humanId: string
}

export function AssetInfoSection({ deviceId, humanId }: AssetInfoSectionProps) {
  const { canOperate } = useAuth()
  const [assignment, setAssignment] = useState<DeviceAssignment | null>(null)
  const [assignErr, setAssignErr] = useState<string | null>(null)
  const [loadingAssignment, setLoadingAssignment] = useState(true)
  const [users, setUsers] = useState<UserDirectoryRow[]>([])
  const [userDirErr, setUserDirErr] = useState<string | null>(null)
  const [pendingUserId, setPendingUserId] = useState('')
  const [savingAssign, setSavingAssign] = useState(false)

  const [tickets, setTickets] = useState<TicketListRow[]>([])
  const [ticketsErr, setTicketsErr] = useState<string | null>(null)
  const [loadingTickets, setLoadingTickets] = useState(true)

  const loadAssignment = useCallback(async () => {
    setAssignErr(null)
    setLoadingAssignment(true)
    try {
      const a = await fetchDeviceAssignment(deviceId)
      setAssignment(a)
    } catch (e) {
      setAssignErr(e instanceof Error ? e.message : 'Failed to load assignment')
      setAssignment(null)
    } finally {
      setLoadingAssignment(false)
    }
  }, [deviceId])

  const loadTickets = useCallback(async () => {
    setTicketsErr(null)
    setLoadingTickets(true)
    try {
      const res = await dashboardFetch(
        `/v1/tickets?device_id=${encodeURIComponent(deviceId)}`,
      )
      if (!res.ok) {
        const j = (await res.json().catch(() => null)) as { error?: string } | null
        throw new Error(j?.error ?? res.statusText)
      }
      const data = (await res.json()) as TicketListRow[]
      setTickets(Array.isArray(data) ? data : [])
    } catch (e) {
      setTicketsErr(e instanceof Error ? e.message : 'Failed to load tickets')
      setTickets([])
    } finally {
      setLoadingTickets(false)
    }
  }, [deviceId])

  useEffect(() => {
    void loadAssignment()
    void loadTickets()
  }, [loadAssignment, loadTickets])

  useEffect(() => {
    if (!canOperate) {
      return
    }
    setUserDirErr(null)
    void (async () => {
      try {
        const list = await fetchUserDirectory()
        setUsers(list)
      } catch (e) {
        setUserDirErr(e instanceof Error ? e.message : 'Failed to load operators')
        setUsers([])
      }
    })()
  }, [canOperate])

  useEffect(() => {
    if (pendingUserId || users.length === 0) {
      return
    }
    setPendingUserId(users[0].id)
  }, [users, pendingUserId])

  async function onAssign() {
    if (!pendingUserId) {
      return
    }
    setSavingAssign(true)
    setAssignErr(null)
    try {
      const next = await postDeviceAssign(deviceId, pendingUserId)
      setAssignment(next)
    } catch (e) {
      setAssignErr(e instanceof Error ? e.message : 'Assign failed')
    } finally {
      setSavingAssign(false)
    }
  }

  async function onUnassign() {
    setSavingAssign(true)
    setAssignErr(null)
    try {
      await postDeviceUnassign(deviceId)
      setAssignment(null)
    } catch (e) {
      setAssignErr(e instanceof Error ? e.message : 'Unassign failed')
    } finally {
      setSavingAssign(false)
    }
  }

  return (
    <div className="mb-6 rounded border border-slate-200 bg-slate-100/80 dark:border-slate-800 dark:bg-slate-900/40 px-3 py-3">
      <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
        <h2 className="text-[11px] font-semibold uppercase tracking-wide text-slate-500">
          Asset info
        </h2>
        <span className="font-mono text-[10px] text-slate-400">{humanId}</span>
      </div>

      {assignErr ? (
        <p className="mb-2 text-[11px] text-rose-400/90">{assignErr}</p>
      ) : null}
      {userDirErr ? (
        <p className="mb-2 text-[11px] text-amber-400/90">{userDirErr}</p>
      ) : null}

      <div className="grid gap-4 md:grid-cols-2">
        <div className="space-y-2">
          <div className="text-[10px] font-semibold uppercase text-slate-500">
            Assigned user
          </div>
          {loadingAssignment ? (
            <div className="text-[12px] text-slate-500">Loading…</div>
          ) : assignment ? (
            <div className="text-[12px] text-slate-200">
              <span className="font-medium text-slate-900 dark:text-slate-100">
                {assignment.username}
              </span>
              <span className="ml-2 font-mono text-[10px] text-slate-500">
                {assignment.user_id}
              </span>
              <div className="mt-1 text-[10px] text-slate-500">
                Since {new Date(assignment.assigned_at).toLocaleString()}
              </div>
            </div>
          ) : (
            <div className="text-[12px] text-slate-500">No user assigned.</div>
          )}
          {canOperate ? (
            <div className="flex flex-wrap items-end gap-2">
              <label className="flex min-w-[180px] flex-1 flex-col gap-0.5 text-[10px] uppercase text-slate-500">
                Operator account
                <select
                  className="rounded border border-slate-300 bg-white dark:border-slate-700 dark:bg-slate-950 px-2 py-1 text-[12px] text-slate-900 dark:text-slate-100"
                  value={pendingUserId}
                  onChange={(e) => setPendingUserId(e.target.value)}
                  disabled={users.length === 0}
                >
                  {users.map((u) => (
                    <option key={u.id} value={u.id}>
                      {u.username}
                    </option>
                  ))}
                </select>
              </label>
              <button
                type="button"
                disabled={savingAssign || users.length === 0 || !pendingUserId}
                onClick={() => void onAssign()}
                className="rounded bg-violet-700 px-3 py-1 text-[11px] font-medium text-white hover:bg-violet-600 disabled:opacity-40"
              >
                Assign
              </button>
              <button
                type="button"
                disabled={savingAssign || !assignment}
                onClick={() => void onUnassign()}
                className="rounded border border-slate-400 bg-transparent px-3 py-1 text-[11px] font-medium text-slate-700 hover:bg-slate-200 disabled:opacity-40 dark:border-slate-600 dark:text-slate-200 dark:hover:bg-slate-800"
              >
                Unassign
              </button>
            </div>
          ) : null}
        </div>

        <div className="min-w-0">
          <div className="mb-2 text-[10px] font-semibold uppercase text-slate-500">
            Device tickets
          </div>
          {ticketsErr ? (
            <p className="text-[11px] text-rose-400/90">{ticketsErr}</p>
          ) : loadingTickets ? (
            <div className="text-[12px] text-slate-500">Loading tickets…</div>
          ) : tickets.length === 0 ? (
            <p className="text-[12px] text-slate-500">
              No open records linked to this device. Create one from the Tickets page.
            </p>
          ) : (
            <ul className="max-h-[200px] space-y-1 overflow-y-auto pr-1 text-[12px]">
              {tickets.slice(0, 25).map((t) => (
                <li
                  key={t.id}
                  className="flex flex-wrap items-baseline gap-2 rounded border border-slate-200/80 px-2 py-1 dark:border-slate-800/80"
                >
                  <Link
                    to="/tickets"
                    className="font-mono text-[11px] text-violet-400 hover:text-violet-300"
                    title="Open Tickets workspace"
                  >
                    {t.ticket_ref}
                  </Link>
                  <span className="min-w-0 flex-1 truncate text-slate-700 dark:text-slate-200">
                    {t.title}
                  </span>
                  <span className="text-[10px] uppercase text-slate-500">{t.status}</span>
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>
    </div>
  )
}
