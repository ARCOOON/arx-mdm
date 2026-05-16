import { dashboardFetch } from './ticketsApi'

export type DeviceCommandRow = {
  id: string
  device_id: string
  command_type: 'ping' | 'reboot' | 'script'
  payload?: string
  status: 'pending' | 'sent' | 'completed' | 'failed'
  output?: string
  created_at: string
  completed_at?: string | null
}

export async function listDeviceCommands(
  deviceId: string,
): Promise<DeviceCommandRow[]> {
  const res = await dashboardFetch(`/v1/devices/${encodeURIComponent(deviceId)}/commands`)
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(
      (err as { error?: string }).error ?? `Failed to load commands (${res.status})`,
    )
  }
  const body = (await res.json()) as { commands: DeviceCommandRow[] }
  return body.commands ?? []
}

export async function queueDeviceCommand(
  deviceId: string,
  commandType: DeviceCommandRow['command_type'],
  payload?: string,
): Promise<DeviceCommandRow> {
  const res = await dashboardFetch(
    `/v1/devices/${encodeURIComponent(deviceId)}/commands`,
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        command_type: commandType,
        payload: payload ?? '',
      }),
    },
  )
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(
      (err as { error?: string }).error ?? `Failed to queue command (${res.status})`,
    )
  }
  return (await res.json()) as DeviceCommandRow
}
