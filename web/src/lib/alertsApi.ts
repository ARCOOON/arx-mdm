import { dashboardFetch } from './ticketsApi'

export type Severity = 'info' | 'warning' | 'critical'

export type ActiveAlertWire = {
  id: string
  alert_rule_id?: string | null
  fingerprint: string
  alert_kind: string
  device_id?: string | null
  severity: Severity | string
  title: string
  message: string
  details: Record<string, unknown>
  triggered_at: string
  last_notified_at?: string | null
  resolved_at?: string | null
}

export type AlertRuleWire = {
  id: string
  name: string
  target_type: string
  metric: string
  operator: string
  threshold: number
  duration_seconds: number
  severity: Severity | string
  is_enabled: boolean
  target_device_id?: string | null
  created_at: string
  updated_at: string
}

export type NotificationChannelWire = {
  id: string
  name: string
  channel_type: 'smtp' | 'webhook' | 'slack'
  config: Record<string, unknown>
  is_active: boolean
  signing_secret_configured: boolean
  created_at: string
  updated_at: string
}

export async function fetchActiveAlerts(params?: {
  limit?: number
  includeResolved?: boolean
}): Promise<ActiveAlertWire[]> {
  const qp = new URLSearchParams()
  if (params?.limit) {
    qp.set('limit', String(params.limit))
  }
  if (params?.includeResolved) {
    qp.set('include_resolved', 'true')
  }
  const suffix = qp.toString()
  const res = await dashboardFetch(`/v1/alerts/active${suffix ? `?${suffix}` : ''}`)
  if (!res.ok) {
    const j = (await res.json().catch(() => null)) as { error?: string } | null
    throw new Error(j?.error ?? res.statusText)
  }
  const data = (await res.json()) as ActiveAlertWire[]
  return Array.isArray(data) ? data : []
}

export async function resolveActiveAlert(id: string): Promise<void> {
  const res = await dashboardFetch(`/v1/alerts/active/${id}/resolve`, { method: 'POST' })
  if (!res.ok) {
    const j = (await res.json().catch(() => null)) as { error?: string } | null
    throw new Error(j?.error ?? res.statusText)
  }
}

export async function fetchAlertRules(): Promise<AlertRuleWire[]> {
  const res = await dashboardFetch('/v1/alerts/rules')
  if (!res.ok) {
    const j = (await res.json().catch(() => null)) as { error?: string } | null
    throw new Error(j?.error ?? res.statusText)
  }
  const data = (await res.json()) as AlertRuleWire[]
  return Array.isArray(data) ? data : []
}

export async function deleteAlertRule(id: string): Promise<void> {
  const res = await dashboardFetch(`/v1/alerts/rules/${id}`, { method: 'DELETE' })
  if (!res.ok) {
    const j = (await res.json().catch(() => null)) as { error?: string } | null
    throw new Error(j?.error ?? res.statusText)
  }
}

export async function patchAlertRulePartial(
  id: string,
  body: Partial<AlertRuleWire> & Record<string, unknown>,
): Promise<void> {
  const res = await dashboardFetch(`/v1/alerts/rules/${id}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!res.ok) {
    const j = (await res.json().catch(() => null)) as { error?: string } | null
    throw new Error(j?.error ?? res.statusText)
  }
}
