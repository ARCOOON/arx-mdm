import { dashboardFetch } from './ticketsApi'

export type BackupWire = {
  filename: string
  size_bytes: number
  created_at: string
}

export async function fetchBackups(): Promise<BackupWire[]> {
  const res = await dashboardFetch('/v1/backups')
  if (!res.ok) {
    const j = (await res.json().catch(() => null)) as { error?: string } | null
    throw new Error(j?.error ?? res.statusText)
  }
  const rows = (await res.json()) as unknown
  if (!Array.isArray(rows)) {
    return []
  }
  return rows as BackupWire[]
}

export async function triggerBackup(): Promise<string> {
  const res = await dashboardFetch('/v1/backups/trigger', {
    method: 'POST',
  })
  const j = (await res.json().catch(() => null)) as { error?: string; filename?: string } | null
  if (!res.ok) {
    throw new Error(j?.error ?? res.statusText)
  }
  if (!j?.filename?.trim()) {
    throw new Error('backup response invalid')
  }
  return j.filename.trim()
}

export function backupDownloadHref(filename: string): string {
  const enc = encodeURIComponent(filename)
  return `/v1/backups/${enc}/download`
}
