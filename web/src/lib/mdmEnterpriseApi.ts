import { dashboardFetch } from './ticketsApi'

export type ConfigurationProfile = {
  id: string
  name: string
  platform: string
  type: string
  payload: Record<string, unknown>
  created_at: string
}

export type PrincipalGroupRow = {
  id: string
  name: string
  description: string
  created_at: string
}

export type ManagedAppConfigRow = {
  id: string
  catalog_app_id: string
  managed_package_name: string
  managed_app_label: string
  config_kv: Record<string, unknown>
  created_at: string
}

export type ProfileAssignmentWire = {
  id: string
  profile_id: string
  target_kind: string
  device_id?: string
  principal_group_id?: string
  created_at: string
}

export type PrincipalGroupDetailEnvelope = {
  group: PrincipalGroupRow
  device_ids: string[]
}

export async function fetchConfigurationProfiles(): Promise<ConfigurationProfile[]> {
  const r = await dashboardFetch('/v1/configuration-profiles')
  if (!r.ok) throw new Error(`profiles ${r.status}`)
  return r.json()
}

export async function createConfigurationProfile(body: unknown): Promise<string> {
  const r = await dashboardFetch('/v1/configuration-profiles', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!r.ok) throw new Error(`profiles create ${r.status}`)
  const j = (await r.json()) as { id?: string }
  if (!j.id) throw new Error('profiles create missing id')
  return j.id
}

export async function fetchPrincipalGroupDetail(id: string): Promise<PrincipalGroupDetailEnvelope> {
  const r = await dashboardFetch(`/v1/principal-groups/${id}`)
  if (!r.ok) throw new Error(`cohort detail ${r.status}`)
  return r.json()
}

export async function fetchProfileAssignments(id: string): Promise<ProfileAssignmentWire[]> {
  const r = await dashboardFetch(`/v1/configuration-profiles/${id}/assignments`)
  if (!r.ok) throw new Error(`assignment list ${r.status}`)
  return r.json()
}

export async function deleteProfile(id: string) {
  const r = await dashboardFetch(`/v1/configuration-profiles/${id}`, { method: 'DELETE' })
  if (!r.ok) throw new Error(`profiles delete ${r.status}`)
}

export async function assignProfile(
  profileId: string,
  payload: Record<string, string>,
): Promise<void> {
  const r = await dashboardFetch(`/v1/configuration-profiles/${profileId}/assignments`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  })
  if (!r.ok) throw new Error(`assign ${r.status}`)
}

export async function deleteAssignment(id: string) {
  const r = await dashboardFetch(`/v1/profile-assignments/${id}`, { method: 'DELETE' })
  if (!r.ok) throw new Error(`assignment delete ${r.status}`)
}

export async function fetchPrincipalGroups(): Promise<PrincipalGroupRow[]> {
  const r = await dashboardFetch('/v1/principal-groups')
  if (!r.ok) throw new Error(`cohorts ${r.status}`)
  return r.json()
}

export async function createPrincipalGroup(payload: Record<string, string>) {
  const r = await dashboardFetch('/v1/principal-groups', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  })
  if (!r.ok) throw new Error(`cohort ${r.status}`)
}

export async function addDevicesToPrincipalGroup(groupId: string, deviceIDs: string[]) {
  const r = await dashboardFetch(`/v1/principal-groups/${groupId}/devices`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ device_ids: deviceIDs }),
  })
  if (!r.ok) throw new Error(`members ${r.status}`)
}

export async function detachDeviceFromPrincipalGroup(groupId: string, deviceId: string) {
  const r = await dashboardFetch(`/v1/principal-groups/${groupId}/devices/${deviceId}`, {
    method: 'DELETE',
  })
  if (!r.ok) throw new Error(`detach ${r.status}`)
}

export async function fetchManagedConfigs(appId: string): Promise<ManagedAppConfigRow[]> {
  const r = await dashboardFetch(`/v1/app-catalog/${appId}/managed-configurations`)
  if (!r.ok) throw new Error(`managed cfg ${r.status}`)
  return r.json()
}

export async function upsertManagedConfig(appId: string, payload: unknown) {
  const r = await dashboardFetch(`/v1/app-catalog/${appId}/managed-configurations`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  })
  if (!r.ok) throw new Error(`managed cfg create ${r.status}`)
}

export async function deleteManagedConfig(appId: string, configId: string) {
  const r = await dashboardFetch(`/v1/app-catalog/${appId}/managed-configurations/${configId}`, {
    method: 'DELETE',
  })
  if (!r.ok) throw new Error(`managed cfg delete ${r.status}`)
}
