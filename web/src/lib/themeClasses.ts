/** Shared light/dark Tailwind utility groups for the consolidated MDM dashboard shell (flat/no-shadow). */

export const shell = {
  page: 'min-h-full bg-gray-50 dark:bg-neutral-950',
  pagePad: 'min-h-full bg-gray-50 px-6 py-4 dark:bg-neutral-950',
  pageBorder:
    'min-h-full border-b border-gray-200 bg-gray-50 px-6 py-4 dark:border-neutral-900 dark:bg-neutral-950',
  content: 'p-6 text-gray-900 dark:text-gray-50',
  contentCompact: 'flex min-h-0 flex-1 flex-col p-4 text-gray-900 dark:text-gray-50',
  card: 'rounded-xl border border-gray-200 bg-white p-4 dark:border-gray-800 dark:bg-neutral-950',
  cardPad: 'rounded-2xl border border-gray-200 bg-white p-5 dark:border-gray-800 dark:bg-neutral-950',
  cardMuted: 'rounded-2xl border border-gray-200 bg-gray-100/85 dark:border-gray-800 dark:bg-neutral-900/55',
  metric:
    'rounded-xl border border-gray-200 bg-white px-4 py-3 dark:border-gray-800 dark:bg-neutral-950',
  tableWrap:
    'min-w-0 overflow-x-auto rounded-2xl border border-gray-200 bg-gray-100/85 dark:border-gray-800 dark:bg-neutral-950/65',
  input:
    'rounded-xl border border-gray-300 bg-white px-2 py-1.5 text-gray-950 outline-none focus-visible:border-gray-500 dark:border-gray-700 dark:bg-neutral-950 dark:text-gray-100 dark:focus-visible:border-gray-400',
  btnSecondary:
    'rounded-xl border border-gray-300 text-gray-800 hover:bg-gray-100 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-neutral-900',
  btnPrimary:
    'rounded-xl bg-gray-900 px-4 py-1.5 text-xs font-semibold text-white hover:bg-gray-700 disabled:opacity-55 dark:bg-white dark:text-neutral-950 dark:hover:bg-neutral-100',
  heading: 'text-lg font-semibold tracking-tight text-gray-950 dark:text-gray-50',
  subheading: 'text-sm font-semibold text-gray-900 dark:text-gray-100',
  label: 'text-[11px] font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400',
  muted: 'text-xs text-gray-600 dark:text-gray-500',
  body: 'text-[12px] text-gray-800 dark:text-gray-300',
  nav: 'flex items-center gap-2 rounded-xl px-2.5 py-1.5 text-[11px] font-semibold text-gray-600 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-neutral-900',
  navActive:
    'border border-gray-300 bg-gray-100 text-gray-950 dark:border-gray-700 dark:bg-neutral-900 dark:text-white',
  navSection: 'mt-4 px-1 text-[10px] font-bold uppercase tracking-widest text-gray-400 dark:text-gray-600',
  inputFull:
    'w-full rounded-xl border border-gray-300 bg-white px-2 py-1.5 text-gray-950 dark:border-gray-700 dark:bg-neutral-950 dark:text-gray-100',
  listItem:
    'rounded-xl border border-gray-200 bg-gray-50 px-2.5 py-2 dark:border-gray-900 dark:bg-neutral-950/65',
  error:
    'rounded-2xl border border-rose-200 bg-rose-50 px-3 py-2 font-mono text-[11px] text-rose-900 dark:border-rose-900/60 dark:bg-rose-950/40 dark:text-rose-200',
  warn: 'rounded-2xl border border-amber-200 bg-amber-50 px-3 py-2 font-mono text-[11px] text-amber-900 dark:border-amber-900/55 dark:bg-amber-950/35 dark:text-amber-50',
} as const
