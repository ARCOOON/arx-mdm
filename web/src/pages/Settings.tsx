import { useCallback, useEffect, useState } from 'react'
import { Navigate } from 'react-router-dom'
import { useAuth } from '../context/AuthContext'
import { dashboardFetch } from '../lib/ticketsApi'
import { Bell, Mail, Send, Webhook } from 'lucide-react'

type AlertSettingRow = {
  id: string
  type: 'smtp' | 'webhook'
  config: Record<string, unknown>
  is_active: boolean
  created_at: string
  updated_at: string
}

function asString(v: unknown): string {
  return typeof v === 'string' ? v : v == null ? '' : String(v)
}

function asStringArray(v: unknown): string[] {
  if (!Array.isArray(v)) {
    return []
  }
  return v.map((x) => (typeof x === 'string' ? x : String(x))).filter(Boolean)
}

export function SettingsPage() {
  const { isAdmin } = useAuth()
  const [rows, setRows] = useState<AlertSettingRow[]>([])
  const [err, setErr] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [msg, setMsg] = useState<string | null>(null)

  const [smtpHost, setSmtpHost] = useState('')
  const [smtpPort, setSmtpPort] = useState('587')
  const [smtpUser, setSmtpUser] = useState('')
  const [smtpPass, setSmtpPass] = useState('')
  const [smtpFrom, setSmtpFrom] = useState('')
  const [smtpTo, setSmtpTo] = useState('')
  const [smtpImplicitTLS, setSmtpImplicitTLS] = useState(false)
  const [smtpSaving, setSmtpSaving] = useState(false)

  const [hookURL, setHookURL] = useState('')
  const [hookSaving, setHookSaving] = useState(false)

  const [testingId, setTestingId] = useState<string | null>(null)

  const load = useCallback(async () => {
    setErr(null)
    setLoading(true)
    try {
      const res = await dashboardFetch('/v1/alerts/settings')
      if (!res.ok) {
        const j = (await res.json().catch(() => null)) as { error?: string } | null
        throw new Error(j?.error ?? res.statusText)
      }
      const data = (await res.json()) as AlertSettingRow[]
      setRows(Array.isArray(data) ? data : [])
    } catch (e) {
      setErr(e instanceof Error ? e.message : 'Failed to load settings')
      setRows([])
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  async function saveSMTP(e: React.FormEvent) {
    e.preventDefault()
    setMsg(null)
    setSmtpSaving(true)
    try {
      const to = smtpTo
        .split(',')
        .map((s) => s.trim())
        .filter(Boolean)
      const res = await dashboardFetch('/v1/alerts/settings', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          type: 'smtp',
          is_active: true,
          config: {
            host: smtpHost.trim(),
            port: Number(smtpPort) || 587,
            username: smtpUser.trim(),
            password: smtpPass,
            from: smtpFrom.trim(),
            to,
            use_implicit_tls: smtpImplicitTLS,
          },
        }),
      })
      const j = (await res.json().catch(() => null)) as { error?: string } | null
      if (!res.ok) {
        throw new Error(j?.error ?? 'Save failed')
      }
      setMsg('SMTP channel saved')
      setSmtpPass('')
      await load()
    } catch (e) {
      setMsg(e instanceof Error ? e.message : 'Save failed')
    } finally {
      setSmtpSaving(false)
    }
  }

  async function saveWebhook(e: React.FormEvent) {
    e.preventDefault()
    setMsg(null)
    setHookSaving(true)
    try {
      const res = await dashboardFetch('/v1/alerts/settings', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          type: 'webhook',
          is_active: true,
          config: {
            url: hookURL.trim(),
            headers: {},
          },
        }),
      })
      const j = (await res.json().catch(() => null)) as { error?: string } | null
      if (!res.ok) {
        throw new Error(j?.error ?? 'Save failed')
      }
      setMsg('Webhook saved')
      setHookURL('')
      await load()
    } catch (e) {
      setMsg(e instanceof Error ? e.message : 'Save failed')
    } finally {
      setHookSaving(false)
    }
  }

  async function toggleActive(id: string, next: boolean) {
    setErr(null)
    const res = await dashboardFetch(`/v1/alerts/settings/${id}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ is_active: next }),
    })
    if (!res.ok) {
      const j = (await res.json().catch(() => null)) as { error?: string } | null
      setErr(j?.error ?? 'Update failed')
      return
    }
    await load()
  }

  async function sendTest(id: string) {
    setMsg(null)
    setTestingId(id)
    try {
      const res = await dashboardFetch(`/v1/alerts/settings/${id}/test`, {
        method: 'POST',
      })
      const j = (await res.json().catch(() => null)) as { error?: string } | null
      if (!res.ok) {
        throw new Error(j?.error ?? 'Test failed')
      }
      setMsg('Test alert dispatched')
    } catch (e) {
      setMsg(e instanceof Error ? e.message : 'Test failed')
    } finally {
      setTestingId(null)
    }
  }

  if (!isAdmin) {
    return <Navigate to="/" replace />
  }

  return (
    <div className="p-6 text-slate-100">
      <div className="flex items-start gap-3">
        <div className="mt-0.5 rounded border border-slate-700 bg-slate-900/80 p-2">
          <Bell className="size-5 text-amber-400/90" />
        </div>
        <div>
          <h1 className="text-lg font-semibold">Settings &amp; alerts</h1>
          <p className="mt-1 max-w-2xl text-xs text-slate-500">
            Configure self-hosted email (SMTP) and outbound webhooks for critical events: stale agent
            heartbeats, Android remote wipe requests, and new{' '}
            <span className="font-mono text-slate-400">INC-</span> tickets.
          </p>
        </div>
      </div>

      {err ? (
        <div className="mt-4 rounded border border-rose-900/60 bg-rose-950/40 px-3 py-2 text-xs text-rose-200">
          {err}
        </div>
      ) : null}
      {msg ? (
        <div className="mt-4 rounded border border-slate-700 bg-slate-900/50 px-3 py-2 text-xs text-slate-300">
          {msg}
        </div>
      ) : null}

      <div className="mt-8 grid gap-8 lg:grid-cols-2">
        <form
          className="space-y-3 rounded border border-slate-800 bg-slate-900/50 p-4"
          onSubmit={saveSMTP}
        >
          <div className="flex items-center gap-2 text-xs font-medium text-slate-400">
            <Mail className="size-3.5" />
            Add SMTP channel
          </div>
          <div className="grid gap-2 sm:grid-cols-2">
            <input
              required
              placeholder="Host"
              className="rounded border border-slate-700 bg-slate-950 px-2 py-1.5 text-sm"
              value={smtpHost}
              onChange={(e) => setSmtpHost(e.target.value)}
            />
            <input
              required
              placeholder="Port"
              className="rounded border border-slate-700 bg-slate-950 px-2 py-1.5 text-sm"
              value={smtpPort}
              onChange={(e) => setSmtpPort(e.target.value)}
            />
            <input
              placeholder="User (optional)"
              className="rounded border border-slate-700 bg-slate-950 px-2 py-1.5 text-sm"
              value={smtpUser}
              onChange={(e) => setSmtpUser(e.target.value)}
            />
            <input
              type="password"
              placeholder="Password (optional)"
              className="rounded border border-slate-700 bg-slate-950 px-2 py-1.5 text-sm"
              value={smtpPass}
              onChange={(e) => setSmtpPass(e.target.value)}
            />
            <input
              required
              placeholder="From address"
              className="rounded border border-slate-700 bg-slate-950 px-2 py-1.5 text-sm sm:col-span-2"
              value={smtpFrom}
              onChange={(e) => setSmtpFrom(e.target.value)}
            />
            <input
              required
              placeholder="To (comma-separated)"
              className="rounded border border-slate-700 bg-slate-950 px-2 py-1.5 text-sm sm:col-span-2"
              value={smtpTo}
              onChange={(e) => setSmtpTo(e.target.value)}
            />
          </div>
          <label className="flex items-center gap-2 text-[11px] text-slate-400">
            <input
              type="checkbox"
              checked={smtpImplicitTLS}
              onChange={(e) => setSmtpImplicitTLS(e.target.checked)}
            />
            Implicit TLS (port 465 / SMTPS)
          </label>
          <button
            type="submit"
            disabled={smtpSaving}
            className="rounded bg-slate-100 px-3 py-1.5 text-xs font-medium text-slate-900 hover:bg-white disabled:opacity-50"
          >
            {smtpSaving ? 'Saving…' : 'Save SMTP'}
          </button>
        </form>

        <form
          className="space-y-3 rounded border border-slate-800 bg-slate-900/50 p-4"
          onSubmit={saveWebhook}
        >
          <div className="flex items-center gap-2 text-xs font-medium text-slate-400">
            <Webhook className="size-3.5" />
            Add webhook
          </div>
          <input
            required
            placeholder="Webhook URL (Slack, Discord, custom…)"
            className="w-full rounded border border-slate-700 bg-slate-950 px-2 py-1.5 text-sm"
            value={hookURL}
            onChange={(e) => setHookURL(e.target.value)}
          />
          <button
            type="submit"
            disabled={hookSaving}
            className="rounded bg-slate-100 px-3 py-1.5 text-xs font-medium text-slate-900 hover:bg-white disabled:opacity-50"
          >
            {hookSaving ? 'Saving…' : 'Save webhook'}
          </button>
        </form>
      </div>

      <div className="mt-10">
        <h2 className="text-sm font-semibold text-slate-200">Configured channels</h2>
        {loading ? (
          <p className="mt-2 text-xs text-slate-500">Loading…</p>
        ) : rows.length === 0 ? (
          <p className="mt-2 text-xs text-slate-500">No alert channels yet.</p>
        ) : (
          <ul className="mt-3 space-y-2">
            {rows.map((r) => (
              <li
                key={r.id}
                className="flex flex-wrap items-center gap-3 rounded border border-slate-800 bg-slate-900/40 px-3 py-2 text-xs"
              >
                <span className="font-mono text-[10px] uppercase text-slate-500">{r.type}</span>
                <span className="text-slate-400">
                  {r.type === 'smtp'
                    ? `${asString(r.config.host)}:${asString(r.config.port)} → ${asStringArray(r.config.to).join(', ') || '(no recipients)'}`
                    : asString(r.config.url)}
                </span>
                <span className="ml-auto flex items-center gap-2">
                  <label className="flex items-center gap-1.5 text-slate-400">
                    <input
                      type="checkbox"
                      checked={r.is_active}
                      onChange={(e) => void toggleActive(r.id, e.target.checked)}
                    />
                    Active
                  </label>
                  <button
                    type="button"
                    className="flex items-center gap-1 rounded border border-slate-600 px-2 py-1 text-[11px] text-slate-200 hover:bg-slate-800 disabled:opacity-50"
                    disabled={testingId === r.id}
                    onClick={() => void sendTest(r.id)}
                    title="Send test alert"
                  >
                    <Send className="size-3.5" />
                    {testingId === r.id ? 'Sending…' : 'Send test alert'}
                  </button>
                </span>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  )
}
