import { useCallback, useEffect, useState } from 'react'
import { Navigate } from 'react-router-dom'
import { useAuth } from '../context/AuthContext'
import { dashboardFetch } from '../lib/ticketsApi'
import { Trash2, UserPlus } from 'lucide-react'

type UserRow = {
  id: string
  username: string
  role: string
  created_at: string
}

export function UsersPage() {
  const { isAdmin, user: me } = useAuth()
  const [rows, setRows] = useState<UserRow[]>([])
  const [err, setErr] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  const [nuName, setNuName] = useState('')
  const [nuPass, setNuPass] = useState('')
  const [nuRole, setNuRole] = useState<'admin' | 'operator' | 'viewer'>('viewer')
  const [creating, setCreating] = useState(false)
  const [createMsg, setCreateMsg] = useState<string | null>(null)

  const load = useCallback(async () => {
    setErr(null)
    setLoading(true)
    try {
      const res = await dashboardFetch('/v1/users')
      if (!res.ok) {
        const j = (await res.json().catch(() => null)) as { error?: string } | null
        throw new Error(j?.error ?? res.statusText)
      }
      const data = (await res.json()) as UserRow[]
      setRows(Array.isArray(data) ? data : [])
    } catch (e) {
      setErr(e instanceof Error ? e.message : 'Failed to load users')
      setRows([])
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  async function createUser(e: React.FormEvent) {
    e.preventDefault()
    setCreateMsg(null)
    setCreating(true)
    try {
      const res = await dashboardFetch('/v1/users', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          username: nuName,
          password: nuPass,
          role: nuRole,
        }),
      })
      const j = (await res.json().catch(() => null)) as { error?: string } | null
      if (!res.ok) {
        throw new Error(j?.error ?? 'Create failed')
      }
      setNuName('')
      setNuPass('')
      setNuRole('viewer')
      setCreateMsg('User created')
      await load()
    } catch (e) {
      setCreateMsg(e instanceof Error ? e.message : 'Create failed')
    } finally {
      setCreating(false)
    }
  }

  async function removeUser(id: string) {
    if (!confirm('Remove this user?')) {
      return
    }
    const res = await dashboardFetch(`/v1/users/${id}`, { method: 'DELETE' })
    if (!res.ok) {
      const j = (await res.json().catch(() => null)) as { error?: string } | null
      alert(j?.error ?? 'Delete failed')
      return
    }
    await load()
  }

  if (!isAdmin) {
    return <Navigate to="/" replace />
  }

  return (
    <div className="p-6 text-slate-900 dark:text-slate-100">
      <h1 className="text-lg font-semibold">User management</h1>
      <p className="mt-1 text-xs text-slate-500">
        Admin-only. Create accounts and assign roles for the dashboard.
      </p>

      <form
        className="mt-6 max-w-md space-y-3 rounded border border-slate-200 bg-white/90 dark:border-slate-800 dark:bg-slate-900/50 p-4"
        onSubmit={createUser}
      >
        <div className="flex items-center gap-2 text-xs font-medium text-slate-400">
          <UserPlus className="size-3.5" />
          Add user
        </div>
        <div className="grid gap-2 sm:grid-cols-2">
          <input
            placeholder="Username"
            className="rounded border border-slate-300 bg-white dark:border-slate-700 dark:bg-slate-950 px-2 py-1.5 text-sm"
            value={nuName}
            onChange={(e) => setNuName(e.target.value)}
            required
          />
          <input
            type="password"
            placeholder="Password"
            className="rounded border border-slate-300 bg-white dark:border-slate-700 dark:bg-slate-950 px-2 py-1.5 text-sm"
            value={nuPass}
            onChange={(e) => setNuPass(e.target.value)}
            required
          />
        </div>
        <select
          className="w-full rounded border border-slate-300 bg-white dark:border-slate-700 dark:bg-slate-950 px-2 py-1.5 text-sm"
          value={nuRole}
          onChange={(e) =>
            setNuRole(e.target.value as 'admin' | 'operator' | 'viewer')
          }
        >
          <option value="viewer">viewer</option>
          <option value="operator">operator</option>
          <option value="admin">admin</option>
        </select>
        <button
          type="submit"
          disabled={creating}
          className="rounded bg-slate-800 px-3 py-1.5 text-xs font-medium text-white hover:bg-slate-700 dark:bg-slate-100 dark:text-slate-900 dark:hover:bg-white disabled:opacity-50"
        >
          {creating ? 'Creating…' : 'Create user'}
        </button>
        {createMsg ? (
          <div className="text-xs text-slate-400">{createMsg}</div>
        ) : null}
      </form>

      <div className="mt-8">
        {loading ? (
          <div className="text-sm text-slate-500">Loading…</div>
        ) : err ? (
          <div className="text-sm text-rose-400">{err}</div>
        ) : (
          <div className="min-w-0 overflow-x-auto rounded border border-slate-200 dark:border-slate-800">
            <table className="min-w-[520px] w-full text-left text-sm">
              <thead className="border-b border-slate-200 bg-slate-100 text-xs uppercase text-slate-500 dark:border-slate-800 dark:bg-slate-900/80">
                <tr>
                  <th className="px-3 py-2">Username</th>
                  <th className="px-3 py-2">Role</th>
                  <th className="px-3 py-2">Created</th>
                  <th className="px-3 py-2" />
                </tr>
              </thead>
              <tbody>
                {rows.map((u) => (
                  <tr key={u.id} className="border-b border-slate-200 dark:border-slate-800/80">
                    <td className="px-3 py-2 font-mono text-xs">{u.username}</td>
                    <td className="px-3 py-2 text-xs text-slate-300">{u.role}</td>
                    <td className="px-3 py-2 text-xs text-slate-500">
                      {new Date(u.created_at).toLocaleString()}
                    </td>
                    <td className="whitespace-nowrap px-3 py-2 text-right align-middle">
                      {u.id !== me?.id ? (
                        <button
                          type="button"
                          className="inline-flex items-center gap-1 rounded px-2 py-1 text-xs text-rose-400 hover:bg-rose-950/40"
                          onClick={() => void removeUser(u.id)}
                        >
                          <Trash2 className="size-3" />
                          Remove
                        </button>
                      ) : (
                        <span className="text-xs text-slate-600">you</span>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  )
}
