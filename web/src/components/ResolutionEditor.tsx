import { useState } from 'react'
import ReactMarkdown from 'react-markdown'

export type ResolutionDoc = {
  id: string
  ticket_id: string
  summary: string
  markdown: string
  resolved_at: string
  created_at: string
}

const mdWrap =
  'max-h-[min(52vh,520px)] overflow-auto rounded border border-slate-200 bg-slate-50 px-2.5 py-2 text-[12px] leading-snug text-slate-800 ' +
  'dark:border-slate-800 dark:bg-slate-950/80 dark:text-slate-200 ' +
  '[&_a]:text-sky-600 [&_a]:dark:text-sky-400 [&_blockquote]:border-l-2 [&_blockquote]:border-slate-300 [&_blockquote]:pl-2 [&_blockquote]:text-slate-600 ' +
  '[&_blockquote]:dark:border-slate-600 [&_blockquote]:dark:text-slate-400 ' +
  '[&_code]:rounded [&_code]:bg-slate-200 [&_code]:px-1 [&_code]:text-[11px] [&_code]:dark:bg-slate-900 ' +
  '[&_h1]:text-sm [&_h1]:font-semibold [&_h1]:text-slate-900 [&_h1]:dark:text-slate-100 ' +
  '[&_h2]:mt-2 [&_h2]:text-[13px] [&_h2]:font-semibold [&_h3]:mt-1.5 [&_h3]:text-[12px] [&_h3]:font-medium [&_li]:my-0.5 [&_ol]:list-decimal [&_ol]:pl-4 ' +
  '[&_p]:my-1 [&_pre]:overflow-x-auto [&_pre]:rounded [&_pre]:bg-slate-100 [&_pre]:p-2 [&_pre]:text-[11px] [&_pre]:dark:bg-slate-900 ' +
  '[&_table]:w-full [&_table]:border-collapse ' +
  '[&_td]:border [&_td]:border-slate-200 [&_td]:px-1.5 [&_td]:py-0.5 [&_td]:dark:border-slate-800 ' +
  '[&_th]:border [&_th]:border-slate-200 [&_th]:px-1.5 [&_th]:py-0.5 [&_th]:text-left [&_th]:dark:border-slate-800 ' +
  '[&_ul]:list-disc [&_ul]:pl-4'

function ViewToggle({
  mode,
  onChange,
}: {
  mode: 'raw' | 'rendered'
  onChange: (m: 'raw' | 'rendered') => void
}) {
  return (
    <div className="inline-flex rounded border border-slate-300 bg-slate-100 p-0.5 text-[11px] dark:border-slate-700 dark:bg-slate-900">
      <button
        type="button"
        className={`rounded px-2 py-0.5 ${
          mode === 'raw'
            ? 'bg-slate-800 text-white dark:bg-slate-700 dark:text-slate-100'
            : 'text-slate-600 hover:text-slate-800 dark:text-slate-500 dark:hover:text-slate-300'
        }`}
        onClick={() => onChange('raw')}
      >
        Raw Markdown
      </button>
      <button
        type="button"
        className={`rounded px-2 py-0.5 ${
          mode === 'rendered'
            ? 'bg-slate-800 text-white dark:bg-slate-700 dark:text-slate-100'
            : 'text-slate-600 hover:text-slate-800 dark:text-slate-500 dark:hover:text-slate-300'
        }`}
        onClick={() => onChange('rendered')}
      >
        Rendered
      </button>
    </div>
  )
}

function ResolutionReadCard({ doc }: { doc: ResolutionDoc }) {
  const [mode, setMode] = useState<'raw' | 'rendered'>('rendered')
  return (
    <div className="border-b border-slate-200 dark:border-slate-800/90 py-2.5 last:border-b-0">
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div>
          <div className="text-[12px] font-medium text-slate-900 dark:text-slate-100">{doc.summary}</div>
          <div className="mt-0.5 font-mono text-[10px] text-slate-500">
            {doc.created_at} · resolved {doc.resolved_at}
          </div>
        </div>
        <ViewToggle mode={mode} onChange={setMode} />
      </div>
      <div className="mt-2">
        {mode === 'raw' ? (
          <pre
            className={`whitespace-pre-wrap font-mono text-[11px] text-slate-300 ${mdWrap}`}
          >
            {doc.markdown || '—'}
          </pre>
        ) : (
          <div className={mdWrap}>
            <ReactMarkdown>{doc.markdown || '_No body._'}</ReactMarkdown>
          </div>
        )}
      </div>
    </div>
  )
}

export function ResolutionEditor(props: {
  resolutions: ResolutionDoc[]
  onSubmit: (payload: { summary: string; markdown: string }) => Promise<void>
  disabled?: boolean
}) {
  const [summary, setSummary] = useState('')
  const [markdown, setMarkdown] = useState('')
  const [mode, setMode] = useState<'raw' | 'rendered'>('raw')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState<string | null>(null)

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setErr(null)
    const s = summary.trim()
    if (!s) {
      setErr('Summary is required.')
      return
    }
    setBusy(true)
    try {
      await props.onSubmit({ summary: s, markdown })
      setSummary('')
      setMarkdown('')
      setMode('raw')
    } catch (e) {
      setErr(e instanceof Error ? e.message : 'Failed to save resolution')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="rounded border border-slate-200 bg-white/90 dark:border-slate-800 dark:bg-slate-900/50">
      <div className="border-b border-slate-200 dark:border-slate-800 px-2.5 py-1.5 text-[11px] font-semibold uppercase tracking-wide text-slate-500">
        Resolutions ({props.resolutions.length})
      </div>
      <div className="max-h-[min(40vh,360px)] overflow-y-auto px-2.5">
        {props.resolutions.length === 0 ? (
          <p className="py-4 text-center text-[11px] text-slate-500">No resolutions yet.</p>
        ) : (
          props.resolutions.map((d) => <ResolutionReadCard key={d.id} doc={d} />)
        )}
      </div>
      <form className="border-t border-slate-800 p-2.5" onSubmit={submit}>
        <div className="mb-1.5 text-[11px] font-semibold uppercase tracking-wide text-slate-500">
          Add resolution
        </div>
        {err ? (
          <p className="mb-2 text-[11px] text-rose-400/90">{err}</p>
        ) : null}
        <input
          className="mb-1.5 w-full rounded border border-slate-300 bg-white dark:border-slate-700 dark:bg-slate-950 px-2 py-1 text-[12px] text-slate-900 outline-none dark:text-slate-100 focus:border-slate-500"
          placeholder="Summary"
          value={summary}
          onChange={(e) => setSummary(e.target.value)}
          disabled={props.disabled || busy}
        />
        <div className="mb-1.5 flex items-center justify-end">
          <ViewToggle mode={mode} onChange={setMode} />
        </div>
        {mode === 'raw' ? (
          <textarea
            className="mb-2 h-36 w-full resize-y rounded border border-slate-300 bg-white dark:border-slate-700 dark:bg-slate-950 px-2 py-1.5 font-mono text-[11px] text-slate-800 outline-none dark:text-slate-200 focus:border-slate-500"
            placeholder="Markdown body"
            value={markdown}
            onChange={(e) => setMarkdown(e.target.value)}
            disabled={props.disabled || busy}
          />
        ) : (
          <div className={`mb-2 min-h-36 ${mdWrap}`}>
            <ReactMarkdown>{markdown || '_Nothing to preview._'}</ReactMarkdown>
          </div>
        )}
        <button
          type="submit"
          disabled={props.disabled || busy}
          className="rounded bg-violet-700 px-2.5 py-1 text-[11px] font-medium text-white hover:bg-violet-600 disabled:opacity-40"
        >
          {busy ? 'Saving…' : 'Post resolution'}
        </button>
      </form>
    </div>
  )
}
