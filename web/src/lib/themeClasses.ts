/** Shared light/dark Tailwind utility groups for the dashboard shell. */

export const shell = {
  page: 'min-h-full bg-slate-50 dark:bg-slate-950',
  pagePad: 'min-h-full bg-slate-50 px-6 py-4 dark:bg-slate-950',
  pageBorder: 'min-h-full border-b border-slate-200/80 bg-slate-50 px-6 py-4 dark:border-slate-800/80 dark:bg-slate-950',
  content: 'p-6 text-slate-900 dark:text-slate-100',
  contentCompact: 'flex min-h-0 flex-1 flex-col p-4 text-slate-900 dark:text-slate-100',
  card: 'rounded border border-slate-200 bg-white/90 p-3 shadow-sm dark:border-slate-800 dark:bg-slate-900/50',
  cardPad: 'rounded border border-slate-200 bg-white/90 p-4 dark:border-slate-800 dark:bg-slate-900/50',
  cardMuted: 'rounded border border-slate-200 bg-slate-100/80 dark:border-slate-800 dark:bg-slate-900/40',
  metric: 'rounded border border-slate-200 bg-white px-3 py-2.5 shadow-sm dark:border-slate-800 dark:bg-slate-900/60',
  tableWrap: 'overflow-hidden rounded border border-slate-200 bg-slate-100/80 dark:border-slate-800 dark:bg-slate-900/40',
  input:
    'rounded border border-slate-300 bg-white px-2 py-1.5 text-slate-900 outline-none focus:border-slate-400 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100 dark:focus:border-slate-500',
  btnSecondary:
    'rounded border border-slate-300 text-slate-700 hover:bg-slate-100 dark:border-slate-700 dark:text-slate-300 dark:hover:bg-slate-800',
  btnPrimary:
    'rounded bg-slate-800 px-3 py-1.5 text-xs font-medium text-white hover:bg-slate-700 disabled:opacity-50 dark:bg-slate-100 dark:text-slate-900 dark:hover:bg-white',
  heading: 'text-lg font-semibold tracking-tight text-slate-900 dark:text-slate-100',
  subheading: 'text-sm font-semibold text-slate-800 dark:text-slate-200',
  label: 'text-[11px] font-semibold uppercase tracking-wide text-slate-500',
  muted: 'text-xs text-slate-600 dark:text-slate-500',
  body: 'text-[12px] text-slate-700 dark:text-slate-300',
  nav: 'flex items-center gap-2 rounded px-2.5 py-1.5 text-xs font-medium text-slate-600 hover:bg-slate-200 hover:text-slate-900 dark:text-slate-300 dark:hover:bg-slate-800 dark:hover:text-white',
  navActive:
    'bg-slate-200 text-slate-900 ring-1 ring-slate-300/80 dark:bg-slate-800 dark:text-white dark:ring-slate-700/80',
  inputFull: 'w-full rounded border border-slate-300 bg-white px-2 py-1.5 text-slate-900 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100',
  listItem:
    'rounded border border-slate-200 bg-slate-50 px-2.5 py-2 dark:border-slate-800/80 dark:bg-slate-950/60',
  error:
    'rounded border border-rose-200 bg-rose-50 px-3 py-2 font-mono text-[11px] text-rose-800 dark:border-rose-900/60 dark:bg-rose-950/40 dark:text-rose-200',
  warn:
    'rounded border border-amber-200 bg-amber-50 px-3 py-2 font-mono text-[11px] text-amber-900 dark:border-amber-900/50 dark:bg-amber-950/30 dark:text-amber-200/90',
} as const
