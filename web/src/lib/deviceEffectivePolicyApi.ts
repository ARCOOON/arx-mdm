import { dashboardFetch } from './ticketsApi'

export type EffectivePolicyConflictProfile = {
  id: string
  name: string
}

export type EffectivePolicySetting = {
  path: string
  value: unknown
  conflict: boolean
  source_profiles?: EffectivePolicyConflictProfile[]
}

export type EffectivePolicySnapshot = {
  asset_id: string
  platform: string
  effective_payload: Record<string, unknown>
  settings: EffectivePolicySetting[]
  conflicts: Array<{
    path: string
    effective_value: unknown
    conflicting_profiles: EffectivePolicyConflictProfile[]
    contributed_normalized?: unknown[]
  }>
  has_conflict: boolean
  revision: string
}

export async function fetchDeviceEffectivePolicy(
  deviceId: string,
): Promise<EffectivePolicySnapshot> {
  const res = await dashboardFetch(
    `/v1/devices/${encodeURIComponent(deviceId)}/effective-policy`,
  )
  if (!res.ok) {
    const text = await res.text().catch(() => '')
    throw new Error(text || `effective-policy HTTP ${res.status}`)
  }
  return res.json() as Promise<EffectivePolicySnapshot>
}
