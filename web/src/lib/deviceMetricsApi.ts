import { dashboardFetch } from './ticketsApi'

export type DeviceMetricSeriesPoint = {
  t: string
  cpu_usage: number
  ram_used_percent: number
}

export type DeviceMetricDisk = {
  total_bytes: number
  used_bytes: number
  sampled_at: string
}

export type DeviceMetricHistory = {
  device_id: string
  hours: number
  bucket_seconds: number
  series: DeviceMetricSeriesPoint[]
  disk?: DeviceMetricDisk
}

export async function fetchDeviceMetrics(
  deviceId: string,
  hours: number,
): Promise<DeviceMetricHistory> {
  const qs =
    hours > 0 && hours !== 24
      ? `?hours=${encodeURIComponent(String(hours))}`
      : ''
  const res = await dashboardFetch(
    `/v1/devices/${encodeURIComponent(deviceId)}/metrics${qs}`,
  )
  if (!res.ok) {
    const text = await res.text().catch(() => '')
    throw new Error(text || `metrics HTTP ${res.status}`)
  }
  return res.json() as Promise<DeviceMetricHistory>
}
