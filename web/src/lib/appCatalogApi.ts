import { dashboardFetch } from './ticketsApi'

export type AppCatalogRow = {
  id: string
  name: string
  version: string
  target_os: string
  file_path_or_url: string
  install_args: string
  created_at: string
}

export type DeviceAppRow = {
  device_id: string
  app_id: string
  app_name: string
  app_version: string
  target_os: string
  status: string
  error_message?: string | null
  last_updated: string
}

export async function fetchAppCatalog(): Promise<AppCatalogRow[]> {
  const r = await dashboardFetch('/v1/app-catalog')
  if (!r.ok) throw new Error(`app-catalog: ${r.status}`)
  return r.json() as Promise<AppCatalogRow[]>
}

export async function uploadAppCatalogEntry(body: FormData): Promise<{ id: string }> {
  const r = await dashboardFetch('/v1/app-catalog/upload', {
    method: 'POST',
    body,
  })
  if (!r.ok) {
    const j = await r.json().catch(() => ({}))
    throw new Error((j as { error?: string }).error ?? `upload: ${r.status}`)
  }
  return r.json() as Promise<{ id: string }>
}

export async function createCatalogFromURL(body: {
  name: string
  version?: string
  target_os: string
  file_path_or_url: string
  install_args?: string
}): Promise<{ id: string }> {
  const r = await dashboardFetch('/v1/app-catalog', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!r.ok) {
    const j = await r.json().catch(() => ({}))
    throw new Error((j as { error?: string }).error ?? `catalog create: ${r.status}`)
  }
  return r.json() as Promise<{ id: string }>
}

export async function patchAppCatalog(
  id: string,
  patch: Record<string, string | undefined>,
): Promise<void> {
  const r = await dashboardFetch(`/v1/app-catalog/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(patch),
  })
  if (!r.ok) {
    const j = await r.json().catch(() => ({}))
    throw new Error((j as { error?: string }).error ?? `catalog patch: ${r.status}`)
  }
}

export async function deleteAppCatalog(id: string): Promise<void> {
  const r = await dashboardFetch(`/v1/app-catalog/${encodeURIComponent(id)}`, {
    method: 'DELETE',
  })
  if (!r.ok) {
    throw new Error(`catalog delete: ${r.status}`)
  }
}

export async function fetchDeviceAppDeployments(
  deviceId: string,
): Promise<DeviceAppRow[]> {
  const r = await dashboardFetch(
    `/v1/devices/${encodeURIComponent(deviceId)}/app-deployments`,
  )
  if (!r.ok) {
    throw new Error(`app-deployments: ${r.status}`)
  }
  const rows = await r.json()
  return rows as DeviceAppRow[]
}

export async function assignAppToDevice(
  deviceId: string,
  appId: string,
): Promise<{ dispatch_succeeded: boolean; dispatch_error?: string | null }> {
  const r = await dashboardFetch(
    `/v1/devices/${encodeURIComponent(deviceId)}/app-deployments`,
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ app_id: appId }),
    },
  )
  if (!r.ok) {
    const j = await r.json().catch(() => ({}))
    throw new Error((j as { error?: string }).error ?? `assign app: ${r.status}`)
  }
  return r.json() as Promise<{
    dispatch_succeeded: boolean
    dispatch_error?: string | null
  }>
}
