import { dashboardFetch } from './ticketsApi'

export type DeviceSecurityResponse = {
  ok: boolean
  device_id: string
  action: string
  dispatched: boolean
}

export async function postDeviceLock(
  deviceId: string,
): Promise<DeviceSecurityResponse> {
  const res = await dashboardFetch(
    `/v1/devices/${encodeURIComponent(deviceId)}/lock`,
    { method: 'POST' },
  )
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(
      (err as { error?: string }).error ?? `Lock failed (${res.status})`,
    )
  }
  return (await res.json()) as DeviceSecurityResponse
}

export async function postDeviceWipe(
  deviceId: string,
): Promise<DeviceSecurityResponse> {
  const res = await dashboardFetch(
    `/v1/devices/${encodeURIComponent(deviceId)}/wipe`,
    { method: 'POST' },
  )
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(
      (err as { error?: string }).error ?? `Wipe failed (${res.status})`,
    )
  }
  return (await res.json()) as DeviceSecurityResponse
}
