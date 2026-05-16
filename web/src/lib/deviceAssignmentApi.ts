import { dashboardFetch } from './ticketsApi'

export type DeviceAssignment = {
  user_id: string
  username: string
  assigned_at: string
}

export type AssignmentResponse =
  | { assignment: DeviceAssignment | null; status?: string }
  | { assignment: DeviceAssignment }

export async function fetchDeviceAssignment(
  deviceId: string,
): Promise<DeviceAssignment | null> {
  const res = await dashboardFetch(`/v1/devices/${encodeURIComponent(deviceId)}/assignment`)
  if (!res.ok) {
    const j = (await res.json().catch(() => null)) as { error?: string } | null
    throw new Error(j?.error ?? res.statusText)
  }
  const data = (await res.json()) as AssignmentResponse
  return 'assignment' in data ? data.assignment : null
}

export async function postDeviceAssign(
  deviceId: string,
  userId: string,
): Promise<DeviceAssignment> {
  const res = await dashboardFetch(`/v1/devices/${encodeURIComponent(deviceId)}/assign`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ user_id: userId }),
  })
  if (!res.ok) {
    const j = (await res.json().catch(() => null)) as { error?: string } | null
    throw new Error(j?.error ?? res.statusText)
  }
  const data = (await res.json()) as { assignment: DeviceAssignment }
  if (!data.assignment) {
    throw new Error('assign response missing assignment')
  }
  return data.assignment
}

export async function postDeviceUnassign(deviceId: string): Promise<void> {
  const res = await dashboardFetch(`/v1/devices/${encodeURIComponent(deviceId)}/unassign`, {
    method: 'POST',
  })
  if (!res.ok) {
    const j = (await res.json().catch(() => null)) as { error?: string } | null
    throw new Error(j?.error ?? res.statusText)
  }
}

export type UserDirectoryRow = {
  id: string
  username: string
}

export async function fetchUserDirectory(): Promise<UserDirectoryRow[]> {
  const res = await dashboardFetch('/v1/users/directory')
  if (!res.ok) {
    const j = (await res.json().catch(() => null)) as { error?: string } | null
    throw new Error(j?.error ?? res.statusText)
  }
  const data = (await res.json()) as UserDirectoryRow[]
  return Array.isArray(data) ? data : []
}
