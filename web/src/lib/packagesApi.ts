import { dashboardFetch } from './ticketsApi'

export type PackageRow = {
  id: string
  name: string
  version: string
  type: string
  install_cmd: string
  created_at: string
  updated_at: string
}

export type DeploymentRow = {
  id: string
  asset_id: string
  asset_human_id: string
  package_id: string
  package_name: string
  package_type: string
  package_version: string
  status: string
  error_message?: string | null
  created_at: string
  updated_at: string
}

export async function fetchPackages(): Promise<PackageRow[]> {
  const r = await dashboardFetch('/v1/packages')
  if (!r.ok) {
    throw new Error(`packages: ${r.status}`)
  }
  return r.json() as Promise<PackageRow[]>
}

export async function createPackage(body: {
  name: string
  version?: string
  type: string
  install_cmd?: string
}): Promise<{ id: string }> {
  const r = await dashboardFetch('/v1/packages', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!r.ok) {
    const j = await r.json().catch(() => ({}))
    throw new Error((j as { error?: string }).error ?? `create package: ${r.status}`)
  }
  return r.json() as Promise<{ id: string }>
}

export async function deletePackage(id: string): Promise<void> {
  const r = await dashboardFetch(`/v1/packages/${encodeURIComponent(id)}`, {
    method: 'DELETE',
  })
  if (!r.ok) {
    throw new Error(`delete package: ${r.status}`)
  }
}

export async function fetchDeployments(
  assetHumanId?: string,
): Promise<DeploymentRow[]> {
  const qs = assetHumanId
    ? `?asset_human_id=${encodeURIComponent(assetHumanId)}`
    : ''
  const r = await dashboardFetch(`/v1/deployments${qs}`)
  if (!r.ok) {
    throw new Error(`deployments: ${r.status}`)
  }
  return r.json() as Promise<DeploymentRow[]>
}

export async function createDeployment(body: {
  asset_human_id: string
  package_id: string
  trigger_deploy: boolean
  operation?: 'install' | 'uninstall'
}): Promise<{
  id: string
  triggered: boolean
  dispatch_succeeded: boolean
  dispatch_error: string | null
}> {
  const r = await dashboardFetch('/v1/deployments', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!r.ok) {
    const j = await r.json().catch(() => ({}))
    throw new Error((j as { error?: string }).error ?? `deployment: ${r.status}`)
  }
  return r.json() as Promise<{
    id: string
    triggered: boolean
    dispatch_succeeded: boolean
    dispatch_error: string | null
  }>
}
