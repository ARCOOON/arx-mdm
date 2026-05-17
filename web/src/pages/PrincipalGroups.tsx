import { useCallback, useEffect, useState } from 'react'
import { shell } from '../lib/themeClasses'
import {
  addDevicesToPrincipalGroup,
  createPrincipalGroup,
  detachDeviceFromPrincipalGroup,
  fetchPrincipalGroupDetail,
  fetchPrincipalGroups,
  type PrincipalGroupRow,
} from '../lib/mdmEnterpriseApi'

export function PrincipalGroupsPage() {
  const [rows, setRows] = useState<PrincipalGroupRow[]>([])
  const [err, setErr] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [devices, setDevices] = useState<string[]>([])

  const [newName, setNewName] = useState('')
  const [newDescription, setNewDescription] = useState('')
  const [bulkDevices, setBulkDevices] = useState('')

  const reloadList = useCallback(async () => {
    setBusy(true)
    setErr(null)
    try {
      const list = await fetchPrincipalGroups()
      setRows(list)
      setSelectedId((prev) => prev ?? list[0]?.id ?? null)
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }, [])

  useEffect(() => {
    void reloadList()
  }, [reloadList])

  const hydrateDetail = useCallback(async (gid: string) => {
    setErr(null)
    try {
      const detail = await fetchPrincipalGroupDetail(gid)
      setDevices(detail.device_ids ?? [])
      setBulkDevices('')
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
      setDevices([])
    }
  }, [])

  useEffect(() => {
    if (selectedId) void hydrateDetail(selectedId)
  }, [selectedId, hydrateDetail])

  const activeGroup = rows.find((g) => g.id === selectedId)

  return (
    <div className={shell.pageBorder}>
      <header className="mb-6 flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 className="text-xl font-semibold tracking-tight text-gray-900 dark:text-gray-50">
            Device cohorts
          </h1>
          <p className="mt-1 max-w-xl text-[12px] text-gray-600 dark:text-gray-400">
            Structured membership groups accelerate profile assignments for enterprise fleet waves.
          </p>
        </div>
        <button
          type="button"
          disabled={busy}
          className={`text-[12px] ${shell.btnSecondary}`}
          onClick={() => void reloadList()}
        >
          Reload
        </button>
      </header>

      {err ? <div className={`mb-4 ${shell.error}`}>{err}</div> : null}

      <section className="grid gap-5 lg:grid-cols-[minmax(260px,0.42fr)_minmax(420px,0.58fr)]">
        <div className="space-y-4">
          <div className={shell.cardPad}>
            <div className={shell.label}>New cohort</div>
            <form
              className="mt-3 space-y-3"
              onSubmit={(e) => {
                e.preventDefault()
                void (async () => {
                  setErr(null)
                  try {
                    await createPrincipalGroup({ name: newName, description: newDescription })
                    setNewName('')
                    setNewDescription('')
                    await reloadList()
                  } catch (ex) {
                    setErr(ex instanceof Error ? ex.message : String(ex))
                  }
                })()
              }}
            >
              <label className="flex flex-col gap-1">
                <span className={shell.label}>Name</span>
                <input
                  className={shell.inputFull}
                  required
                  value={newName}
                  onChange={(e) => setNewName(e.target.value)}
                />
              </label>
              <label className="flex flex-col gap-1">
                <span className={shell.label}>Description</span>
                <textarea
                  rows={3}
                  className={`${shell.inputFull} text-[13px]`}
                  value={newDescription}
                  onChange={(e) => setNewDescription(e.target.value)}
                />
              </label>
              <button className={`w-full py-2 text-[13px] ${shell.btnPrimary}`} type="submit">
                Create cohort
              </button>
            </form>
          </div>
          <div className={shell.cardPad}>
            <div className={shell.label}>All cohorts</div>
            <div className="mt-3 max-h-[28rem] space-y-1 overflow-y-auto rounded-xl border border-gray-100 dark:border-neutral-900/60">
              {rows.map((g) => (
                <button
                  key={g.id}
                  type="button"
                  onClick={() => setSelectedId(g.id)}
                  className={`flex w-full flex-col border-b border-gray-100 px-3 py-2 text-left text-[13px] last:border-none dark:border-gray-900/60 ${
                    selectedId === g.id
                      ? 'bg-gray-100 dark:bg-neutral-900/70'
                      : 'hover:bg-gray-50 dark:hover:bg-neutral-950'
                  }`}
                >
                  <span className="font-semibold text-gray-950 dark:text-gray-50">{g.name}</span>
                  <span className="font-mono text-[10px] text-gray-500">{g.id}</span>
                </button>
              ))}
              {rows.length === 0 ? (
                <div className="px-3 py-4 text-center text-[12px] text-gray-500">
                  No cohorts yet.
                </div>
              ) : null}
            </div>
          </div>
        </div>

        <div className={shell.cardPad}>
          {!activeGroup ? (
            <p className={`${shell.body}`}>Select a cohort to manage membership.</p>
          ) : (
            <>
              <div className="flex flex-wrap items-start justify-between gap-3 border-b border-dashed border-gray-200 pb-4 dark:border-gray-800">
                <div>
                  <div className={shell.label}>Active cohort</div>
                  <div className="text-[16px] font-semibold text-gray-950 dark:text-gray-50">
                    {activeGroup.name}
                  </div>
                  <p className="mt-2 text-[12px] text-gray-600 dark:text-gray-400">
                    {activeGroup.description || 'No description captured.'}
                  </p>
                  <p className="mt-2 font-mono text-[11px] text-gray-600 dark:text-gray-400">{activeGroup.id}</p>
                </div>
              </div>
              <div className="mt-6">
                <div className={`${shell.label}`}>Mapped assets ({devices.length})</div>
                <div className="mt-2 max-h-60 space-y-2 overflow-y-auto rounded-xl border border-gray-100 dark:border-gray-900">
                  {devices.length === 0 ? (
                    <p className="px-3 py-3 text-[11px] text-gray-600">Nothing mapped.</p>
                  ) : (
                    devices.map((id) => (
                      <div
                        key={id}
                        className="flex items-center gap-3 border-b border-gray-50 px-3 py-2 last:border-none dark:border-gray-900"
                      >
                        <span className="flex-1 font-mono text-[11px] text-gray-800 dark:text-gray-200">
                          {id}
                        </span>
                        <button
                          type="button"
                          className={`text-[11px] ${shell.btnSecondary}`}
                          onClick={() =>
                            void detachDeviceFromPrincipalGroup(activeGroup.id, id).then(() =>
                              hydrateDetail(activeGroup.id),
                            )
                          }
                        >
                          Detach
                        </button>
                      </div>
                    ))
                  )}
                </div>

                <div className="mt-5 space-y-2 rounded-xl border border-gray-200 bg-gray-50/80 p-3 dark:border-gray-800 dark:bg-neutral-950/40">
                  <label className="flex flex-col gap-1">
                    <span className={shell.label}>Bulk asset UUID ingest</span>
                    <textarea
                      rows={4}
                      className={`${shell.inputFull} font-mono text-[11px]`}
                      placeholder="One UUID per line"
                      value={bulkDevices}
                      onChange={(e) => setBulkDevices(e.target.value)}
                    />
                  </label>
                  <button
                    type="button"
                    className={`w-full justify-center py-2 text-[12px] ${shell.btnPrimary}`}
                    onClick={() =>
                      void (async () => {
                        try {
                          const ids = bulkDevices
                            .split(/\r?\n/)
                            .map((s) => s.trim())
                            .filter(Boolean)
                          if (!selectedId || ids.length === 0) return
                          await addDevicesToPrincipalGroup(selectedId, ids)
                          await hydrateDetail(selectedId)
                        } catch (ex) {
                          setErr(ex instanceof Error ? ex.message : String(ex))
                        }
                      })()
                    }
                  >
                    Enroll memberships
                  </button>
                </div>
              </div>
            </>
          )}
        </div>
      </section>
    </div>
  )
}
