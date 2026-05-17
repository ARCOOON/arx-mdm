import type { AssetRow } from '../types/ws'

export function ComplianceBadge({ asset }: { asset: AssetRow }) {
  const raw = (asset.compliance_status ?? '').trim().toLowerCase()
  const isol = asset.quarantine_enabled === true
  let label = 'Evaluating'
  let cls =
    'border-amber-700/80 text-amber-200 bg-amber-950/15 dark:bg-amber-950/20'
  if (raw === 'compliant') {
    label = 'Compliant'
    cls =
      'border-emerald-700/80 text-emerald-900 bg-emerald-50 dark:bg-emerald-950/25 dark:text-emerald-100'
  } else if (raw === 'non_compliant') {
    label = 'Non-compliant'
    cls =
      'border-rose-700/90 text-rose-900 bg-rose-50 dark:bg-rose-950/25 dark:text-rose-100'
  }
  const tip = asset.compliance_reason?.trim() || undefined
  return (
    <div className="flex flex-col gap-0.5">
      <span
        title={tip}
        className={`inline-flex w-fit rounded border px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide ${cls}`}
      >
        {label}
      </span>
      {isol ? (
        <span className="inline-flex w-fit rounded border border-violet-700/70 bg-violet-950/15 px-1.5 py-0.5 text-[9px] font-semibold uppercase tracking-wide text-violet-900 dark:text-violet-100">
          Isolated
        </span>
      ) : null}
    </div>
  )
}
