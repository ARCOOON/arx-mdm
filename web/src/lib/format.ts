export function formatBytes(n: number): string {
  if (!Number.isFinite(n) || n <= 0) {
    return '0 B'
  }
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let v = n
  let u = 0
  while (v >= 1024 && u < units.length - 1) {
    v /= 1024
    u++
  }
  const digits = u === 0 ? 0 : u === 1 ? 0 : 1
  return `${v.toFixed(digits)} ${units[u]}`
}

export function formatBytesPair(used: number, total: number): string {
  if (total <= 0) {
    return formatBytes(used)
  }
  const pct = Math.min(100, Math.round((used / total) * 100))
  return `${formatBytes(used)} / ${formatBytes(total)} (${pct}%)`
}

export function formatCpu(model: string, cores: number, pct: number): string {
  const shortModel =
    model.length > 42 ? `${model.slice(0, 40)}…` : model || '—'
  const p = Number.isFinite(pct) ? pct.toFixed(1) : '0.0'
  return `${shortModel} · ${cores}c @ ${p}%`
}
