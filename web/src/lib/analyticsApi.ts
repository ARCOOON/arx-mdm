import { dashboardFetch } from './ticketsApi'

export type AnalyticsSummary = {
  online_threshold_seconds: number
  assets: {
    total: number
    online: number
    offline: number
  }
  os_distribution: Record<string, number>
  incidents: {
    unresolved: number
  }
}

export async function fetchAnalyticsSummary(): Promise<AnalyticsSummary> {
  const r = await dashboardFetch('/v1/analytics/summary')
  if (!r.ok) {
    throw new Error(`analytics: ${r.status}`)
  }
  return r.json() as Promise<AnalyticsSummary>
}
