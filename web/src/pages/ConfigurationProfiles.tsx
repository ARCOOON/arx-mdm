import { useCallback, useEffect, useMemo, useState } from 'react'
import { shell } from '../lib/themeClasses'
import {
  assignProfile,
  createConfigurationProfile,
  deleteAssignment,
  deleteProfile,
  fetchConfigurationProfiles,
  fetchProfileAssignments,
  type ConfigurationProfile,
  type ProfileAssignmentWire,
} from '../lib/mdmEnterpriseApi'

export function ConfigurationProfilesPage() {
  const [rows, setRows] = useState<ConfigurationProfile[]>([])
  const [err, setErr] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [expanded, setExpanded] = useState<string | null>(null)
  const [assignments, setAssignments] = useState<ProfileAssignmentWire[]>([])

  const [draftName, setDraftName] = useState('')
  const [draftPlatform, setDraftPlatform] = useState('windows')
  const [draftType, setDraftType] = useState('restrictions')
  const [draftPayload, setDraftPayload] = useState<string>('{}')
  const [assignKind, setAssignKind] = useState<'device' | 'principal_group'>(
    'device',
  )
  const [assignTarget, setAssignTarget] = useState('')

  const reload = useCallback(async () => {
    setErr(null)
    setBusy(true)
    try {
      const list = await fetchConfigurationProfiles()
      setRows(list)
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
      setRows([])
    } finally {
      setBusy(false)
    }
  }, [])

  useEffect(() => {
    void reload()
  }, [reload])

  const platformLabel = useMemo(() => {
    return (p: ConfigurationProfile) => `${p.platform} · ${p.type}`
  }, [])

  const loadAssignments = useCallback(async (id: string) => {
    setErr(null)
    try {
      const list = await fetchProfileAssignments(id)
      setAssignments(list)
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
      setAssignments([])
    }
  }, [])

  const toggleExpand = useCallback(
    async (id: string) => {
      if (expanded === id) {
        setExpanded(null)
        setAssignments([])
        return
      }
      setExpanded(id)
      await loadAssignments(id)
    },
    [expanded, loadAssignments],
  )

  return (
    <div className={shell.pageBorder}>
      <div className="mb-6 flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 className="text-xl font-semibold tracking-tight text-gray-900 dark:text-gray-50">
            Configuration profiles
          </h1>
          <p className="mt-1 max-w-xl text-[12px] text-gray-600 dark:text-gray-400">
            Declarative device policies assigned to cohorts or individual assets, synced over mTLS
            to enrolled endpoints on telemetry and HTTPS pull cadences for continuous remediation.
          </p>
        </div>
        <button type="button" className={`text-[12px] ${shell.btnSecondary}`} onClick={() => void reload()} disabled={busy}>
          Reload
        </button>
      </div>

      {err ? <div className={`mb-4 ${shell.error}`}>{err}</div> : null}

      <section className="mb-10 grid gap-5 lg:grid-cols-[minmax(0,1.05fr)_minmax(340px,0.95fr)]">
        <div className={shell.cardPad}>
          <header className="mb-4">
            <div className={shell.label}>Inventory</div>
            <div className={shell.subheading}>Published profiles ({rows.length})</div>
          </header>
          <div className="space-y-2">
            {rows.length === 0 && !busy ? (
              <p className={`${shell.body} px-2`}>No declarative payloads yet.</p>
            ) : null}
            {rows.map((p) => {
              const active = expanded === p.id
              return (
                <article
                  key={p.id}
                  className="rounded-xl border border-gray-200 bg-gray-50/80 p-3 dark:border-gray-800 dark:bg-neutral-950/40"
                >
                  <button
                    type="button"
                    className="flex w-full flex-col gap-2 text-left"
                    onClick={() => void toggleExpand(p.id)}
                  >
                    <div className="flex flex-wrap items-center gap-3">
                      <div
                        className={`${shell.body} flex-1 font-semibold text-gray-950 dark:text-gray-50`}
                      >
                        {p.name}
                      </div>
                      <span className="rounded-lg border border-gray-200 px-2 py-0.5 font-mono text-[10px] uppercase text-gray-700 dark:border-gray-700 dark:text-gray-200">
                        {platformLabel(p)}
                      </span>
                    </div>
                    <div className="font-mono text-[11px] text-gray-600 dark:text-gray-400">{p.id}</div>
                  </button>
                  {active ? (
                    <div className="mt-3 space-y-3 border-t border-dashed border-gray-200 pt-3 dark:border-gray-800">
                      <div className="flex flex-wrap gap-3">
                        <button
                          type="button"
                          className={`text-[11px] ${shell.btnSecondary}`}
                          onClick={() =>
                            navigator.clipboard.writeText(JSON.stringify(p.payload, null, 2))
                          }
                        >
                          Copy payload
                        </button>
                        <button
                          type="button"
                          className="text-[11px] rounded-xl border border-rose-200 px-3 py-1.5 font-medium text-rose-900 hover:bg-rose-50 dark:border-rose-900/70 dark:text-rose-200 dark:hover:bg-rose-950/60"
                          onClick={() => void deleteProfile(p.id).then(reload)}
                        >
                          Delete
                        </button>
                      </div>
                      <div>
                        <div className={`${shell.label}`}>Assignments</div>
                        <div className="mt-2 space-y-2">
                          {assignments.length === 0 ? (
                            <p className={shell.body}>Nothing linked.</p>
                          ) : (
                            assignments.map((a) => (
                              <div
                                key={a.id}
                                className="flex flex-wrap items-center justify-between gap-2 rounded-xl border border-gray-200 px-3 py-2 dark:border-gray-800"
                              >
                                <div className="text-[11px] text-gray-800 dark:text-gray-200">
                                  <span className="uppercase">{a.target_kind}</span>
                                  {a.device_id ? (
                                    <span className="ml-2 font-mono">{a.device_id}</span>
                                  ) : (
                                    <span className="ml-2 font-mono">{a.principal_group_id}</span>
                                  )}
                                </div>
                                <button
                                  type="button"
                                  className={`text-[11px] ${shell.btnSecondary}`}
                                  onClick={() =>
                                    void deleteAssignment(a.id).then(() => loadAssignments(p.id))
                                  }
                                >
                                  Remove
                                </button>
                              </div>
                            ))
                          )}
                        </div>
                        <div className="mt-4 space-y-2 rounded-xl border border-gray-200 bg-white p-3 dark:border-gray-800 dark:bg-neutral-950/60">
                          <label className="flex flex-col gap-1">
                            <span className={`${shell.label}`}>Assignment target kind</span>
                            <select
                              className={shell.input}
                              value={assignKind}
                              onChange={(e) =>
                                setAssignKind(e.target.value as typeof assignKind)
                              }
                            >
                              <option value="device">Device asset UUID</option>
                              <option value="principal_group">Device cohort UUID</option>
                            </select>
                          </label>
                          <label className="flex flex-col gap-1">
                            <span className={`${shell.label}`}>Destination UUID</span>
                            <input
                              value={assignTarget}
                              onChange={(e) => setAssignTarget(e.target.value.trim())}
                              className={`${shell.input}`}
                              placeholder={
                                assignKind === 'device'
                                  ? 'xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx'
                                  : 'cohort UUID'
                              }
                            />
                          </label>
                          <button
                            type="button"
                            className={`w-full justify-center py-2 text-[12px] ${shell.btnPrimary}`}
                            onClick={() =>
                              void assignProfile(
                                p.id,
                                assignKind === 'device'
                                  ? {
                                      target_kind: 'device',
                                      device_id: assignTarget,
                                    }
                                  : {
                                      target_kind: 'principal_group',
                                      principal_group_id: assignTarget,
                                    },
                              )
                                .then(() => loadAssignments(p.id))
                                .catch((e: unknown) =>
                                  setErr(e instanceof Error ? e.message : String(e)),
                                )
                            }
                          >
                            Attach assignment
                          </button>
                        </div>
                      </div>
                    </div>
                  ) : null}
                </article>
              )
            })}
          </div>
        </div>

        <div className={shell.cardPad}>
          <div className={shell.label}>Composer</div>
          <div className="mt-1 text-[14px] font-semibold text-gray-900 dark:text-gray-50">
            Create profile envelope
          </div>
          <p className="mt-2 text-[12px] text-gray-600 dark:text-gray-400">
            JSON payloads mirror the declarative dictionaries consumed by enrolled agents across
            Windows registry paths, Linux `/proc/sys` tunables, and Android `user_restrictions`.
          </p>
          <form
            className="mt-4 space-y-3"
            onSubmit={(e) => {
              e.preventDefault()
              void (async () => {
                setErr(null)
                try {
                  const payload = JSON.parse(draftPayload || '{}') as Record<string, unknown>
                  await createConfigurationProfile({
                    name: draftName,
                    platform: draftPlatform,
                    type: draftType,
                    payload,
                  })
                  setDraftName('')
                  await reload()
                } catch (exc) {
                  setErr(exc instanceof Error ? exc.message : String(exc))
                }
              })()
            }}
          >
            <label className="flex flex-col gap-1">
              <span className={shell.label}>Name</span>
              <input className={shell.inputFull} value={draftName} onChange={(e) => setDraftName(e.target.value)} required />
            </label>
            <div className="grid gap-3 sm:grid-cols-2">
              <label className="flex flex-col gap-1">
                <span className={shell.label}>Platform</span>
                <select className={shell.input} value={draftPlatform} onChange={(e) => setDraftPlatform(e.target.value)}>
                  <option value="windows">windows</option>
                  <option value="linux">linux</option>
                  <option value="android">android</option>
                </select>
              </label>
              <label className="flex flex-col gap-1">
                <span className={shell.label}>Type</span>
                <input
                  className={shell.input}
                  value={draftType}
                  onChange={(e) => setDraftType(e.target.value)}
                  placeholder="restrictions · firewall · wifi · custom"
                  required
                />
              </label>
            </div>
            <label className="flex flex-col gap-1">
              <span className={shell.label}>JSON payload</span>
              <textarea
                rows={14}
                className="rounded-xl border border-gray-200 bg-white px-2 py-1.5 font-mono text-[11px] text-gray-900 outline-none dark:border-gray-700 dark:bg-neutral-950 dark:text-gray-50"
                value={draftPayload}
                onChange={(e) => setDraftPayload(e.target.value)}
              />
            </label>
            <button className={`w-full justify-center py-2 text-[13px] ${shell.btnPrimary}`} type="submit">
              Persist profile
            </button>
          </form>
        </div>
      </section>
    </div>
  )
}
