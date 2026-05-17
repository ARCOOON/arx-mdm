import { useCallback, useEffect, useMemo, useState } from 'react'
import ReactMarkdown from 'react-markdown'
import { BookOpen, FilePlus, Save, Trash2, X } from 'lucide-react'
import { dashboardFetch } from '../lib/ticketsApi'
import { useAuth } from '../context/AuthContext'

type DocRow = {
  id: string
  title: string
  content_markdown: string
  uploaded_by?: string | null
  created_at: string
}

export function KnowledgeBasePage() {
  const { canOperate } = useAuth()
  const [docs, setDocs] = useState<DocRow[]>([])
  const [err, setErr] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  const [editorOpen, setEditorOpen] = useState(false)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [title, setTitle] = useState('')
  const [md, setMd] = useState('')
  const [saving, setSaving] = useState(false)
  const [saveErr, setSaveErr] = useState<string | null>(null)
  const [readOnlyDoc, setReadOnlyDoc] = useState<DocRow | null>(null)

  const load = useCallback(async () => {
    setErr(null)
    setLoading(true)
    try {
      const res = await dashboardFetch('/v1/documents')
      if (!res.ok) {
        const j = (await res.json().catch(() => null)) as { error?: string } | null
        throw new Error(j?.error ?? res.statusText)
      }
      const data = (await res.json()) as DocRow[]
      setDocs(Array.isArray(data) ? data : [])
    } catch (e) {
      setErr(e instanceof Error ? e.message : 'Failed to load documents')
      setDocs([])
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  function openNew() {
    setEditingId(null)
    setTitle('')
    setMd('# New guide\n\n')
    setSaveErr(null)
    setEditorOpen(true)
  }

  function openEdit(d: DocRow) {
    setEditingId(d.id)
    setTitle(d.title)
    setMd(d.content_markdown)
    setSaveErr(null)
    setEditorOpen(true)
  }

  function openView(d: DocRow) {
    if (canOperate) {
      openEdit(d)
    } else {
      setReadOnlyDoc(d)
    }
  }

  async function saveDoc() {
    setSaveErr(null)
    setSaving(true)
    try {
      if (editingId) {
        const res = await dashboardFetch(`/v1/documents/${editingId}`, {
          method: 'PATCH',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ title, content_markdown: md }),
        })
        if (!res.ok) {
          const j = (await res.json().catch(() => null)) as { error?: string } | null
          throw new Error(j?.error ?? 'Save failed')
        }
      } else {
        const res = await dashboardFetch('/v1/documents', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ title, content_markdown: md }),
        })
        if (!res.ok) {
          const j = (await res.json().catch(() => null)) as { error?: string } | null
          throw new Error(j?.error ?? 'Create failed')
        }
      }
      setEditorOpen(false)
      await load()
    } catch (e) {
      setSaveErr(e instanceof Error ? e.message : 'Save failed')
    } finally {
      setSaving(false)
    }
  }

  async function deleteDoc(id: string) {
    if (!confirm('Delete this document?')) {
      return
    }
    const res = await dashboardFetch(`/v1/documents/${id}`, { method: 'DELETE' })
    if (!res.ok) {
      const j = (await res.json().catch(() => null)) as { error?: string } | null
      alert(j?.error ?? 'Delete failed')
      return
    }
    await load()
  }

  const preview = useMemo(() => md, [md])

  return (
    <div className="p-6 text-slate-900 dark:text-slate-100">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-lg font-semibold">Knowledge base</h1>
          <p className="mt-1 text-xs text-slate-500">
            Internal guides and procedures (Markdown).
          </p>
        </div>
        {canOperate ? (
          <button
            type="button"
            onClick={openNew}
            className="inline-flex items-center gap-2 rounded bg-slate-800 px-3 py-1.5 text-xs font-medium text-white hover:bg-slate-700 dark:bg-slate-100 dark:text-slate-900 dark:hover:bg-white"
          >
            <FilePlus className="size-3.5" />
            New document
          </button>
        ) : null}
      </div>

      {loading ? (
        <div className="mt-8 text-sm text-slate-500">Loading…</div>
      ) : err ? (
        <div className="mt-8 text-sm text-rose-400">{err}</div>
      ) : (
        <div className="mt-6 grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {docs.map((d) => (
            <article
              key={d.id}
              className="flex flex-col rounded border border-slate-200 bg-slate-100/80 dark:border-slate-800 dark:bg-slate-900/40 p-3"
            >
              <div className="flex items-start gap-2">
                <BookOpen className="mt-0.5 size-4 shrink-0 text-slate-500" />
                <div className="min-w-0 flex-1">
                  <h2 className="truncate text-sm font-medium text-slate-900 dark:text-slate-100">{d.title}</h2>
                  <p className="mt-1 text-[10px] text-slate-500">
                    {new Date(d.created_at).toLocaleString()}
                  </p>
                </div>
              </div>
              <p className="mt-2 line-clamp-3 text-xs text-slate-400">{d.content_markdown}</p>
              <div className="mt-3 flex flex-wrap gap-2">
                <button
                  type="button"
                  className="rounded border border-slate-300 dark:border-slate-700 px-2 py-1 text-[11px] text-slate-700 hover:bg-slate-200 dark:text-slate-300 dark:hover:bg-slate-200 dark:hover:bg-slate-800"
                  onClick={() => openView(d)}
                >
                  {canOperate ? 'Open' : 'View'}
                </button>
                {canOperate ? (
                  <>
                    <button
                      type="button"
                      className="inline-flex items-center gap-1 rounded border border-rose-900/50 px-2 py-1 text-[11px] text-rose-300 hover:bg-rose-950/30"
                      onClick={() => void deleteDoc(d.id)}
                    >
                      <Trash2 className="size-3" />
                      Delete
                    </button>
                  </>
                ) : null}
              </div>
            </article>
          ))}
        </div>
      )}

      {editorOpen ? (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-3 sm:p-4">
          <div className="flex max-h-[min(100dvh,100vh)] w-full max-w-5xl flex-col overflow-hidden rounded-lg border border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-950">
            <div className="flex items-center justify-between border-b border-slate-200 dark:border-slate-800 px-4 py-2">
              <span className="text-sm font-medium text-slate-200">
                {editingId ? 'Edit document' : 'New document'}
              </span>
              <button
                type="button"
                className="rounded p-1 text-slate-500 hover:bg-slate-800 hover:text-slate-200"
                onClick={() => setEditorOpen(false)}
              >
                <X className="size-4" />
              </button>
            </div>
            <div className="grid min-h-0 flex-1 gap-0 md:grid-cols-2">
              <div className="flex min-h-0 flex-col border-b border-slate-200 dark:border-slate-800 p-3 md:border-b-0 md:border-r">
                <input
                  className="mb-2 rounded border border-slate-300 bg-white px-2 py-1.5 text-sm text-slate-900 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-100"
                  placeholder="Title"
                  value={title}
                  onChange={(e) => setTitle(e.target.value)}
                />
                <textarea
                  className="min-h-[280px] flex-1 resize-none rounded border border-slate-300 bg-white p-2 font-mono text-xs leading-relaxed text-slate-900 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-100"
                  placeholder="Markdown content"
                  value={md}
                  onChange={(e) => setMd(e.target.value)}
                />
              </div>
              <div className="min-h-0 overflow-auto p-3 text-sm leading-relaxed text-slate-700 dark:text-slate-300 [&_a]:text-sky-600 [&_a]:dark:text-sky-400 [&_code]:rounded [&_code]:bg-slate-200 [&_code]:px-1 [&_code]:dark:bg-slate-800 [&_pre]:overflow-x-auto [&_pre]:rounded [&_pre]:bg-slate-100 [&_pre]:p-2 [&_pre]:dark:bg-slate-900 [&_ul]:list-disc [&_ul]:pl-5">
                <ReactMarkdown>{preview}</ReactMarkdown>
              </div>
            </div>
            <div className="flex items-center justify-between border-t border-slate-200 px-4 py-2 dark:border-slate-800">
              {saveErr ? (
                <span className="text-xs text-rose-400">{saveErr}</span>
              ) : (
                <span />
              )}
              {canOperate ? (
                <button
                  type="button"
                  disabled={saving || !title.trim()}
                  onClick={() => void saveDoc()}
                  className="inline-flex items-center gap-2 rounded bg-emerald-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-emerald-500 disabled:opacity-50"
                >
                  <Save className="size-3.5" />
                  {saving ? 'Saving…' : 'Save'}
                </button>
              ) : null}
            </div>
          </div>
        </div>
      ) : null}

      {readOnlyDoc ? (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-3 sm:p-4">
          <div className="max-h-[min(100dvh,100vh)] w-full max-w-2xl overflow-hidden rounded-lg border border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-950">
            <div className="flex items-center justify-between border-b border-slate-200 dark:border-slate-800 px-4 py-2">
              <span className="text-sm font-medium text-slate-200">{readOnlyDoc.title}</span>
              <button
                type="button"
                className="rounded p-1 text-slate-500 hover:bg-slate-800 hover:text-slate-200"
                onClick={() => setReadOnlyDoc(null)}
              >
                <X className="size-4" />
              </button>
            </div>
            <div className="max-h-[calc(min(100dvh,100vh)-6rem)] overflow-y-auto overscroll-contain p-4 text-sm leading-relaxed text-slate-700 dark:text-slate-300 [&_a]:text-sky-600 [&_a]:dark:text-sky-400 [&_code]:rounded [&_code]:bg-slate-200 [&_code]:px-1 [&_code]:dark:bg-slate-800 [&_pre]:overflow-x-auto [&_pre]:rounded [&_pre]:bg-slate-100 [&_pre]:p-2 [&_pre]:dark:bg-slate-900 [&_ul]:list-disc [&_ul]:pl-5">
              <ReactMarkdown>{readOnlyDoc.content_markdown}</ReactMarkdown>
            </div>
          </div>
        </div>
      ) : null}
    </div>
  )
}
