import { dashboardFetch } from './ticketsApi'

export type PatchDeviceQuarantineResponse = {
  quarantine_enabled: boolean
  command_id: string
  dispatched: boolean
}

export async function patchDeviceQuarantine(
  deviceId: string,
  quarantineEnabled: boolean,
): Promise<PatchDeviceQuarantineResponse> {
  const res = await dashboardFetch(
    `/v1/devices/${encodeURIComponent(deviceId)}/quarantine`,
    {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ quarantine_enabled: quarantineEnabled }),
    },
  )
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(
      (err as { error?: string }).error ??
        `Quarantine update failed (${res.status})`,
    )
  }
  return (await res.json()) as PatchDeviceQuarantineResponse
}
