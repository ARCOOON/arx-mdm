import { dashboardFetch } from './ticketsApi'

export type AutomationRow = {
  id: string
  name: string
  cron_schedule: string
  action_type: string
  target_os?: string | null
  target_asset_id?: string | null
  payload_json: Record<string, unknown>
  is_active: boolean
  created_at: string
  updated_at: string
}

export async function fetchAutomations(): Promise<AutomationRow[]> {
  const r = await dashboardFetch('/v1/automations')
  if (!r.ok) {
    throw new Error(`automations: ${r.status}`)
  }
  return r.json() as Promise<AutomationRow[]>
}

export async function createAutomation(body: {
  name: string
  cron_schedule: string
  action_type: 'shutdown' | 'deploy_package'
  target_os?: string
  target_asset_id?: string
  payload_json?: Record<string, unknown>
  is_active?: boolean
}): Promise<{ id: string }> {
  const r = await dashboardFetch('/v1/automations', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!r.ok) {
    const t = await r.text()
    throw new Error(t || `automations create: ${r.status}`)
  }
  return r.json() as Promise<{ id: string }>
}

export async function patchAutomation(
  id: string,
  body: { is_active: boolean },
): Promise<void> {
  const r = await dashboardFetch(`/v1/automations/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!r.ok) {
    const t = await r.text()
    throw new Error(t || `automations patch: ${r.status}`)
  }
}
