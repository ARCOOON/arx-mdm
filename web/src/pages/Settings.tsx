import { useCallback, useEffect, useState } from 'react'
import { Link, Navigate } from 'react-router-dom'
import { Bell, Flame, Mail, Send, Trash2, Webhook } from 'lucide-react'
import { useAuth } from '../context/AuthContext'
import {
  deleteAlertRule,
  fetchAlertRules,
  patchAlertRulePartial,
  type AlertRuleWire,
  type NotificationChannelWire,
} from '../lib/alertsApi'
import { dashboardFetch } from '../lib/ticketsApi'
import { shell } from '../lib/themeClasses'

function asString(v: unknown): string {
  return typeof v === 'string' ? v : v == null ? '' : String(v)
}

function asStringArray(v: unknown): string[] {
  if (!Array.isArray(v)) {
    return []
  }
  return v.map((x) => (typeof x === 'string' ? x : String(x))).filter(Boolean)
}

const METRICS = ['cpu_usage', 'ram_usage_percent', 'disk_usage_percent', 'offline_status'] as const
const OPS = ['>', '<', '>=', '<=', '=='] as const
const SEV = ['info', 'warning', 'critical'] as const

export function SettingsPage() {
  const { isAdmin } = useAuth()

  const [channels, setChannels] = useState<NotificationChannelWire[]>([])
  const [rules, setRules] = useState<AlertRuleWire[]>([])
  const [err, setErr] = useState<string | null>(null)
  const [msg, setMsg] = useState<string | null>(null)
  const [loadingChannels, setLoadingChannels] = useState(true)
  const [loadingRules, setLoadingRules] = useState(true)

  const [smtpHost, setSmtpHost] = useState('')
  const [smtpPort, setSmtpPort] = useState('587')
  const [smtpUser, setSmtpUser] = useState('')
  const [smtpPass, setSmtpPass] = useState('')
  const [smtpFrom, setSmtpFrom] = useState('')
  const [smtpTo, setSmtpTo] = useState('')
  const [smtpImplicitTLS, setSmtpImplicitTLS] = useState(false)
  const [smtpSaving, setSmtpSaving] = useState(false)

  const [hookURL, setHookURL] = useState('')
  const [hookSecret, setHookSecret] = useState('')
  const [hookSaving, setHookSaving] = useState(false)

  const [slackURL, setSlackURL] = useState('')
  const [slackSecret, setSlackSecret] = useState('')
  const [slackSaving, setSlackSaving] = useState(false)

  const [ruleName, setRuleName] = useState('High sustained CPU load')
  const [ruleMetric, setRuleMetric] = useState<(typeof METRICS)[number]>('cpu_usage')
  const [ruleOp, setRuleOp] = useState<(typeof OPS)[number]>('>')
  const [ruleThreshold, setRuleThreshold] = useState('85')
  const [ruleDur, setRuleDur] = useState('300')
  const [ruleSev, setRuleSev] = useState<(typeof SEV)[number]>('warning')
  const [ruleAsset, setRuleAsset] = useState('')
  const [ruleSaving, setRuleSaving] = useState(false)

  const [testingId, setTestingId] = useState<string | null>(null)

  const loadChannels = useCallback(async () => {
    setErr(null)
    setLoadingChannels(true)
    try {
      const res = await dashboardFetch('/v1/alerts/settings')
      if (!res.ok) {
        const j = (await res.json().catch(() => null)) as { error?: string } | null
        throw new Error(j?.error ?? res.statusText)
      }
      const data = (await res.json()) as NotificationChannelWire[]
      setChannels(Array.isArray(data) ? data : [])
    } catch (e) {
      setErr(e instanceof Error ? e.message : 'Failed to load channels')
      setChannels([])
    } finally {
      setLoadingChannels(false)
    }
  }, [])

  const loadRules = useCallback(async () => {
    setErr(null)
    setLoadingRules(true)
    try {
      const rows = await fetchAlertRules()
      setRules(rows)
    } catch (e) {
      setErr(e instanceof Error ? e.message : 'Failed to load alert rules')
      setRules([])
    } finally {
      setLoadingRules(false)
    }
  }, [])

  useEffect(() => {
    void loadChannels()
    void loadRules()
  }, [loadChannels, loadRules])

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
          name: 'SMTP channel',
          type: 'smtp',
          channel_type: 'smtp',
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
      await loadChannels()
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
          name: 'Outbound webhook',
          channel_type: 'webhook',
          is_active: true,
          signing_secret: hookSecret,
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
      setHookSecret('')
      await loadChannels()
    } catch (e) {
      setMsg(e instanceof Error ? e.message : 'Save failed')
    } finally {
      setHookSaving(false)
    }
  }

  async function saveSlack(e: React.FormEvent) {
    e.preventDefault()
    setMsg(null)
    setSlackSaving(true)
    try {
      const res = await dashboardFetch('/v1/alerts/settings', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          name: 'Slack Incoming Webhook',
          channel_type: 'slack',
          is_active: true,
          signing_secret: slackSecret,
          config: {
            url: slackURL.trim(),
            headers: {},
          },
        }),
      })
      const j = (await res.json().catch(() => null)) as { error?: string } | null
      if (!res.ok) {
        throw new Error(j?.error ?? 'Save failed')
      }
      setMsg('Slack webhook saved')
      setSlackURL('')
      setSlackSecret('')
      await loadChannels()
    } catch (e) {
      setMsg(e instanceof Error ? e.message : 'Save failed')
    } finally {
      setSlackSaving(false)
    }
  }

  async function toggleChannelActive(id: string, next: boolean) {
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
    await loadChannels()
  }

  async function sendChannelTest(id: string) {
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
      setMsg('Test notification dispatched')
    } catch (e) {
      setMsg(e instanceof Error ? e.message : 'Test failed')
    } finally {
      setTestingId(null)
    }
  }

  async function createRule(ev: React.FormEvent) {
    ev.preventDefault()
    setRuleSaving(true)
    setMsg(null)
    try {
      const secs = Number(ruleDur)
      const thr = Number(ruleThreshold)
      if (!ruleName.trim() || !Number.isFinite(secs) || secs <= 0 || !Number.isFinite(thr)) {
        throw new Error('Name, numeric threshold, and positive duration_seconds are required')
      }
      const payload: Record<string, unknown> = {
        name: ruleName.trim(),
        target_type: 'device',
        metric: ruleMetric,
        operator: ruleOp,
        threshold: thr,
        duration_seconds: Math.floor(secs),
        severity: ruleSev,
        is_enabled: true,
      }
      const tgt = ruleAsset.trim()
      if (tgt !== '') {
        payload.target_device_id = tgt
      }
      const res = await dashboardFetch('/v1/alerts/rules', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      })
      const j = (await res.json().catch(() => null)) as { error?: string } | null
      if (!res.ok) {
        throw new Error(j?.error ?? 'Failed to save rule')
      }
      setMsg('Alert rule created')
      await loadRules()
    } catch (e) {
      setMsg(e instanceof Error ? e.message : 'Failed')
    } finally {
      setRuleSaving(false)
    }
  }

  async function toggleRule(rule: AlertRuleWire, next: boolean) {
    await patchAlertRulePartial(rule.id, { is_enabled: next })
    await loadRules()
  }

  async function destroyRule(id: string) {
    await deleteAlertRule(id)
    await loadRules()
  }

  if (!isAdmin) {
    return <Navigate to="/" replace />
  }

  return (
    <div className="p-6 text-slate-900 dark:text-slate-100">
      <div className="flex items-start gap-3">
        <div className="mt-0.5 rounded border border-slate-300 dark:border-slate-700 bg-slate-900/80 p-2">
          <Bell className="size-5 text-amber-400/90" />
        </div>
        <div>
          <h1 className="text-lg font-semibold">Settings &mdash; alerting</h1>
          <p className="mt-1 max-w-3xl text-xs text-slate-500">
            Define Prometheus-style thresholds, route notifications through SMTP, Slack-compatible webhooks, or
            signed generic JSON webhooks. The alerting engine evaluates metrics every thirty seconds plus a built-in
            five-minute offline watchdog. Automated backup controls live under{' '}
            <Link className="text-sky-600 hover:underline dark:text-sky-400" to="/settings/backups">
              Settings / Backup bundles
            </Link>
            .
          </p>
        </div>
      </div>

      {err ? <div className={`mt-4 ${shell.error}`}>{err}</div> : null}
      {msg ? (
        <div className="mt-4 rounded border border-slate-300 bg-slate-100 px-3 py-2 text-xs text-slate-800 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200">
          {msg}
        </div>
      ) : null}

      <section className="mt-10">
        <h2 className={`${shell.subheading}`}>Alert rules</h2>
        <p className="mt-1 text-[11px] text-slate-500">
          Device-scoped telemetry windows use rolling averages sampled from <span className="font-mono">device_metrics</span>.
        </p>
        <div className="mt-5 grid gap-6 lg:grid-cols-[minmax(0,340px)_1fr]">
          <form
            className={`${shell.cardPad} space-y-2`}
            onSubmit={(e) => void createRule(e)}
          >
            <div className={`${shell.label}`}>Compose rule</div>
            <input
              required
              className={shell.inputFull}
              value={ruleName}
              placeholder="Friendly name"
              onChange={(e) => setRuleName(e.target.value)}
            />
            <div className="grid gap-2 sm:grid-cols-2">
              <select
                className={shell.input}
                value={ruleMetric}
                onChange={(e) => setRuleMetric(e.target.value as (typeof METRICS)[number])}
              >
                {METRICS.map((m) => (
                  <option key={m} value={m}>
                    {m}
                  </option>
                ))}
              </select>
              <select
                className={shell.input}
                value={ruleOp}
                onChange={(e) => setRuleOp(e.target.value as (typeof OPS)[number])}
              >
                {OPS.map((op) => (
                  <option key={op} value={op}>
                    {op}
                  </option>
                ))}
              </select>
            </div>
            <div className="grid gap-2 sm:grid-cols-2">
              <input
                required
                className={shell.inputFull}
                value={ruleThreshold}
                placeholder="Threshold"
                onChange={(e) => setRuleThreshold(e.target.value)}
              />
              <input
                required
                className={shell.inputFull}
                title="Rolling window aggregation length in seconds"
                value={ruleDur}
                placeholder="duration_seconds"
                onChange={(e) => setRuleDur(e.target.value)}
              />
            </div>
            <select
              className={shell.inputFull}
              value={ruleSev}
              onChange={(e) => setRuleSev(e.target.value as (typeof SEV)[number])}
            >
              {SEV.map((s) => (
                <option key={s} value={s}>
                  {s}
                </option>
              ))}
            </select>
            <input
              className={shell.inputFull}
              placeholder="Optional asset UUID scope"
              value={ruleAsset}
              onChange={(e) => setRuleAsset(e.target.value)}
            />
            <button type="submit" disabled={ruleSaving} className={shell.btnPrimary}>
              {ruleSaving ? 'Saving…' : 'Save rule'}
            </button>
          </form>

          <div className={`${shell.tableWrap}`}>
            {loadingRules ? (
              <div className="p-4 text-xs text-slate-500">Loading rules…</div>
            ) : rules.length === 0 ? (
              <div className="p-4 text-xs text-slate-500">No rules yet.</div>
            ) : (
              <table className="w-full border-collapse text-left text-[11px]">
                <thead className="bg-slate-200/70 text-[10px] uppercase tracking-wide dark:bg-slate-900">
                  <tr>
                    <th className="px-3 py-2 font-semibold">Name</th>
                    <th className="px-3 py-2 font-semibold">Metric</th>
                    <th className="px-3 py-2 font-semibold">Logic</th>
                    <th className="px-3 py-2 font-semibold">Severity</th>
                    <th className="px-3 py-2 font-semibold">Active</th>
                    <th className="px-3 py-2 font-semibold" />
                  </tr>
                </thead>
                <tbody>
                  {rules.map((r) => (
                    <tr key={r.id} className="border-t border-slate-200 dark:border-slate-800">
                      <td className="px-3 py-2 align-top">{r.name}</td>
                      <td className="px-3 py-2 align-top font-mono text-[10px]">{r.metric}</td>
                      <td className="px-3 py-2 align-top font-mono text-[10px]">
                        avg {r.duration_seconds}s {r.operator} {r.threshold}
                      </td>
                      <td className="px-3 py-2 align-top capitalize">{String(r.severity)}</td>
                      <td className="px-3 py-2 align-top">
                        <input
                          type="checkbox"
                          checked={r.is_enabled}
                          onChange={(ev) => void toggleRule(r, ev.target.checked)}
                        />
                      </td>
                      <td className="px-3 py-2 align-top text-right">
                        <button
                          type="button"
                          title="Remove rule"
                          className={`inline-flex rounded border px-2 py-1 ${shell.btnSecondary}`}
                          onClick={() => void destroyRule(r.id)}
                        >
                          <Trash2 className="size-3.5" />
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>
        </div>
      </section>

      <section className="mt-14">
        <h2 className={`${shell.subheading}`}>Notification routes</h2>
        <p className="mt-1 max-w-2xl text-[11px] text-slate-500">
          Web deliveries include optional HMAC signatures using the UTF-8 shared secret persisted per channel (<span className="font-mono">Arx-Timestamp</span>{' '}
          + canonical body).
        </p>

        <div className="mt-6 grid gap-8 xl:grid-cols-3">
          <form className={`${shell.cardPad} space-y-3`} onSubmit={saveSMTP}>
            <div className="flex items-center gap-2 text-xs font-medium text-slate-400">
              <Mail className="size-3.5" />
              SMTP
            </div>
            <div className="grid gap-2 sm:grid-cols-2">
              <input required placeholder="Host" className={shell.inputFull} value={smtpHost} onChange={(e) => setSmtpHost(e.target.value)} />
              <input required placeholder="Port" className={shell.inputFull} value={smtpPort} onChange={(e) => setSmtpPort(e.target.value)} />
              <input placeholder="Username" className={shell.inputFull} value={smtpUser} onChange={(e) => setSmtpUser(e.target.value)} />
              <input type="password" placeholder="Password" className={shell.inputFull} value={smtpPass} onChange={(e) => setSmtpPass(e.target.value)} />
              <input required placeholder="From" className={shell.inputFull + ' sm:col-span-2'} value={smtpFrom} onChange={(e) => setSmtpFrom(e.target.value)} />
              <input
                required
                placeholder="Recipients (comma separated)"
                className={shell.inputFull + ' sm:col-span-2'}
                value={smtpTo}
                onChange={(e) => setSmtpTo(e.target.value)}
              />
            </div>
            <label className="flex items-center gap-2 text-[11px] text-slate-400">
              <input type="checkbox" checked={smtpImplicitTLS} onChange={(e) => setSmtpImplicitTLS(e.target.checked)} />
              Implicit TLS / SMTPS (465)
            </label>
            <button type="submit" disabled={smtpSaving} className={shell.btnPrimary}>
              {smtpSaving ? 'Saving…' : 'Add SMTP'}
            </button>
          </form>

          <form className={`${shell.cardPad} space-y-3`} onSubmit={saveWebhook}>
            <div className="flex items-center gap-2 text-xs font-medium text-slate-400">
              <Webhook className="size-3.5" />
              Webhook
            </div>
            <input
              required
              placeholder="https://example.com/hooks/mdm-alerts"
              className={shell.inputFull}
              value={hookURL}
              onChange={(e) => setHookURL(e.target.value)}
            />
            <input
              placeholder="Signing secret (HMAC SHA-256)"
              className={shell.inputFull}
              value={hookSecret}
              onChange={(e) => setHookSecret(e.target.value)}
            />
            <button type="submit" disabled={hookSaving} className={shell.btnPrimary}>
              {hookSaving ? 'Saving…' : 'Save webhook'}
            </button>
          </form>

          <form className={`${shell.cardPad} space-y-3`} onSubmit={saveSlack}>
            <div className="flex items-center gap-2 text-xs font-medium text-slate-400">
              <Flame className="size-3.5 text-rose-400" />
              Slack Incoming Webhook
            </div>
            <input
              required
              placeholder="Slack Incoming Webhook URL"
              className={shell.inputFull}
              value={slackURL}
              onChange={(e) => setSlackURL(e.target.value)}
            />
            <input
              placeholder="Signing secret (optional)"
              className={shell.inputFull}
              value={slackSecret}
              onChange={(e) => setSlackSecret(e.target.value)}
            />
            <button type="submit" disabled={slackSaving} className={shell.btnPrimary}>
              {slackSaving ? 'Saving…' : 'Save Slack route'}
            </button>
          </form>
        </div>
      </section>

      <section className="mt-14">
        <h2 className={`${shell.subheading}`}>Configured routes</h2>
        {loadingChannels ? (
          <p className="mt-2 text-xs text-slate-500">Loading…</p>
        ) : channels.length === 0 ? (
          <p className="mt-2 text-xs text-slate-500">No channels configured.</p>
        ) : (
          <ul className="mt-3 space-y-2">
            {channels.map((c) => (
              <li key={c.id} className={shell.listItem + ' flex flex-wrap items-center gap-3'}>
                <span className="font-mono text-[10px] uppercase text-slate-500">{c.channel_type}</span>
                <span className="min-w-[120px] text-slate-300">{c.name}</span>
                <span className="text-slate-400">
                  {c.channel_type === 'smtp'
                    ? `${asString(c.config.host)}:${asString(c.config.port)} → ${asStringArray(c.config.to).join(', ') || '(no recipients)'}`
                    : asString((c.config as Record<string, unknown>).url)}
                </span>
                <span className="ml-auto flex flex-wrap items-center gap-2">
                  {c.signing_secret_configured ? (
                    <span className="rounded border border-slate-600 px-1.5 py-0 font-mono text-[9px] text-slate-400">
                      HMAC armed
                    </span>
                  ) : null}
                  <label className="flex items-center gap-1.5 text-slate-400">
                    <input
                      type="checkbox"
                      checked={c.is_active}
                      onChange={(e) => void toggleChannelActive(c.id, e.target.checked)}
                    />
                    Active
                  </label>
                  <button
                    type="button"
                    className={`flex items-center gap-1 rounded border border-slate-400 px-2 py-1 text-[11px] ${shell.btnSecondary}`}
                    disabled={testingId === c.id}
                    onClick={() => void sendChannelTest(c.id)}
                  >
                    <Send className="size-3.5" />
                    {testingId === c.id ? 'Sending…' : 'Ping'}
                  </button>
                </span>
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  )
}
