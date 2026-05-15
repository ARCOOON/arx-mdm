import { useCallback, useEffect, useState, type FormEvent } from 'react'
import { useWebSocket } from '../hooks/useWebSocket'
import {
  createDeployment,
  createPackage,
  deletePackage,
  fetchDeployments,
  fetchPackages,
  type DeploymentRow,
  type PackageRow,
} from '../lib/packagesApi'

const PKG_TYPES = ['winget', 'apt', 'dnf', 'choco', 'custom'] as const

export function SoftwarePage() {
  const { assets } = useWebSocket()
  const [packages, setPackages] = useState<PackageRow[]>([])
  const [deployments, setDeployments] = useState<DeploymentRow[]>([])
  const [err, setErr] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  const [newName, setNewName] = useState('')
  const [newVer, setNewVer] = useState('')
  const [newType, setNewType] = useState<string>('winget')
  const [newCmd, setNewCmd] = useState('')

  const [selAsset, setSelAsset] = useState('')
  const [selPkg, setSelPkg] = useState('')
  const [trigger, setTrigger] = useState(true)
  const [depOp, setDepOp] = useState<'install' | 'uninstall'>('install')

  const reload = useCallback(async () => {
    setErr(null)
    const [pkgs, deps] = await Promise.all([
      fetchPackages(),
      fetchDeployments(),
    ])
    setPackages(pkgs)
    setDeployments(deps)
  }, [])

  useEffect(() => {
    reload().catch((e) => {
      setErr(e instanceof Error ? e.message : String(e))
    })
  }, [reload])

  async function onCreatePackage(e: FormEvent) {
    e.preventDefault()
    setBusy(true)
    setErr(null)
    try {
      await createPackage({
        name: newName.trim(),
        version: newVer.trim(),
        type: newType,
        install_cmd: newCmd.trim(),
      })
      setNewName('')
      setNewVer('')
      setNewCmd('')
      await reload()
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  async function onAssign(e: FormEvent) {
    e.preventDefault()
    setBusy(true)
    setErr(null)
    try {
      const res = await createDeployment({
        asset_human_id: selAsset,
        package_id: selPkg,
        trigger_deploy: trigger,
        operation: depOp,
      })
      if (res.dispatch_error) {
        setErr(`Deployment created; dispatch: ${res.dispatch_error}`)
      }
      await reload()
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  async function onDeletePkg(id: string) {
    if (!window.confirm('Delete this package catalog entry?')) {
      return
    }
    setBusy(true)
    setErr(null)
    try {
      await deletePackage(id)
      await reload()
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="min-h-full bg-slate-950 px-6 py-4 text-slate-200">
      <h1 className="mb-1 text-lg font-semibold tracking-tight text-slate-100">
        Software deployments
      </h1>
      <p className="mb-6 max-w-2xl text-[12px] text-slate-500">
        Define catalog packages and assign them to assets. When “Deploy immediately”
        is checked, the server pushes a C2 command to the agent (requires the asset
        online).
      </p>

      {err ? (
        <div className="mb-4 rounded border border-rose-900/60 bg-rose-950/40 px-3 py-2 text-[12px] text-rose-200">
          {err}
        </div>
      ) : null}

      <div className="mb-8 grid gap-6 lg:grid-cols-2">
        <section className="rounded border border-slate-800 bg-slate-900/30 p-4">
          <h2 className="mb-3 text-[11px] font-semibold uppercase text-slate-500">
            New catalog package
          </h2>
          <form className="flex flex-col gap-2 text-[12px]" onSubmit={onCreatePackage}>
            <label className="flex flex-col gap-1">
              <span className="text-slate-500">Name / id</span>
              <input
                required
                value={newName}
                onChange={(e) => setNewName(e.target.value)}
                className="rounded border border-slate-700 bg-slate-950 px-2 py-1.5 font-mono text-slate-100"
                placeholder="e.g. Microsoft.VisualStudioCode"
              />
            </label>
            <label className="flex flex-col gap-1">
              <span className="text-slate-500">Version (optional)</span>
              <input
                value={newVer}
                onChange={(e) => setNewVer(e.target.value)}
                className="rounded border border-slate-700 bg-slate-950 px-2 py-1.5 font-mono text-slate-100"
                placeholder="empty = default / latest"
              />
            </label>
            <label className="flex flex-col gap-1">
              <span className="text-slate-500">Type</span>
              <select
                value={newType}
                onChange={(e) => setNewType(e.target.value)}
                className="rounded border border-slate-700 bg-slate-950 px-2 py-1.5 text-slate-100"
              >
                {PKG_TYPES.map((t) => (
                  <option key={t} value={t}>
                    {t}
                  </option>
                ))}
              </select>
            </label>
            {newType === 'custom' ? (
              <label className="flex flex-col gap-1">
                <span className="text-slate-500">install_cmd (argv, absolute binary)</span>
                <input
                  required
                  value={newCmd}
                  onChange={(e) => setNewCmd(e.target.value)}
                  className="rounded border border-slate-700 bg-slate-950 px-2 py-1.5 font-mono text-slate-100"
                  placeholder="/usr/local/bin/my-installer --flag"
                />
              </label>
            ) : null}
            <button
              type="submit"
              disabled={busy}
              className="mt-1 w-fit rounded bg-sky-600 px-3 py-1.5 text-[12px] font-medium text-white hover:bg-sky-500 disabled:opacity-50"
            >
              Add package
            </button>
          </form>
        </section>

        <section className="rounded border border-slate-800 bg-slate-900/30 p-4">
          <h2 className="mb-3 text-[11px] font-semibold uppercase text-slate-500">
            Assign to asset
          </h2>
          <form className="flex flex-col gap-2 text-[12px]" onSubmit={onAssign}>
            <label className="flex flex-col gap-1">
              <span className="text-slate-500">Asset</span>
              <select
                required
                value={selAsset}
                onChange={(e) => setSelAsset(e.target.value)}
                className="rounded border border-slate-700 bg-slate-950 px-2 py-1.5 text-slate-100"
              >
                <option value="">Select…</option>
                {assets.map((a) => (
                  <option key={a.human_id} value={a.human_id}>
                    {a.human_id} — {a.hostname || 'no hostname'}
                  </option>
                ))}
              </select>
            </label>
            <label className="flex flex-col gap-1">
              <span className="text-slate-500">Package</span>
              <select
                required
                value={selPkg}
                onChange={(e) => setSelPkg(e.target.value)}
                className="rounded border border-slate-700 bg-slate-950 px-2 py-1.5 text-slate-100"
              >
                <option value="">Select…</option>
                {packages.map((p) => (
                  <option key={p.id} value={p.id}>
                    [{p.type}] {p.name}
                  </option>
                ))}
              </select>
            </label>
            <label className="flex items-center gap-2 text-slate-400">
              <input
                type="checkbox"
                checked={trigger}
                onChange={(e) => setTrigger(e.target.checked)}
              />
              Deploy immediately (C2)
            </label>
            <label className="flex flex-col gap-1">
              <span className="text-slate-500">Operation</span>
              <select
                value={depOp}
                onChange={(e) =>
                  setDepOp(e.target.value as 'install' | 'uninstall')
                }
                className="rounded border border-slate-700 bg-slate-950 px-2 py-1.5 text-slate-100"
              >
                <option value="install">install</option>
                <option value="uninstall">uninstall</option>
              </select>
            </label>
            <button
              type="submit"
              disabled={busy}
              className="mt-1 w-fit rounded bg-emerald-700 px-3 py-1.5 text-[12px] font-medium text-white hover:bg-emerald-600 disabled:opacity-50"
            >
              Create deployment
            </button>
          </form>
        </section>
      </div>

      <section className="mb-8">
        <h2 className="mb-2 text-[11px] font-semibold uppercase text-slate-500">
          Catalog
        </h2>
        <div className="overflow-x-auto rounded border border-slate-800">
          <table className="w-full border-collapse text-left text-[11px]">
            <thead className="bg-slate-900/80 text-slate-500">
              <tr>
                <th className="border-b border-slate-800 px-2 py-2">Type</th>
                <th className="border-b border-slate-800 px-2 py-2">Name</th>
                <th className="border-b border-slate-800 px-2 py-2">Version</th>
                <th className="border-b border-slate-800 px-2 py-2">install_cmd</th>
                <th className="border-b border-slate-800 px-2 py-2" />
              </tr>
            </thead>
            <tbody className="text-slate-300">
              {packages.map((p) => (
                <tr key={p.id} className="border-b border-slate-800/80">
                  <td className="px-2 py-1.5 font-mono text-sky-300/90">{p.type}</td>
                  <td className="px-2 py-1.5 font-mono">{p.name}</td>
                  <td className="px-2 py-1.5 font-mono text-slate-500">
                    {p.version || '—'}
                  </td>
                  <td className="max-w-[200px] truncate px-2 py-1.5 font-mono text-slate-500">
                    {p.install_cmd || '—'}
                  </td>
                  <td className="px-2 py-1.5 text-right">
                    <button
                      type="button"
                      className="text-rose-400 hover:text-rose-300"
                      onClick={() => onDeletePkg(p.id)}
                      disabled={busy}
                    >
                      Delete
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {packages.length === 0 ? (
            <div className="px-3 py-4 text-[12px] text-slate-600">No packages yet.</div>
          ) : null}
        </div>
      </section>

      <section>
        <h2 className="mb-2 text-[11px] font-semibold uppercase text-slate-500">
          Recent deployments
        </h2>
        <div className="overflow-x-auto rounded border border-slate-800">
          <table className="w-full border-collapse text-left text-[11px]">
            <thead className="bg-slate-900/80 text-slate-500">
              <tr>
                <th className="border-b border-slate-800 px-2 py-2">When</th>
                <th className="border-b border-slate-800 px-2 py-2">Asset</th>
                <th className="border-b border-slate-800 px-2 py-2">Package</th>
                <th className="border-b border-slate-800 px-2 py-2">Status</th>
                <th className="border-b border-slate-800 px-2 py-2">Error</th>
              </tr>
            </thead>
            <tbody className="text-slate-300">
              {deployments.map((d) => (
                <tr key={d.id} className="border-b border-slate-800/80">
                  <td className="whitespace-nowrap px-2 py-1.5 text-slate-500">
                    {new Date(d.created_at).toLocaleString()}
                  </td>
                  <td className="px-2 py-1.5 font-mono">{d.asset_human_id}</td>
                  <td className="px-2 py-1.5 font-mono">
                    [{d.package_type}] {d.package_name}
                  </td>
                  <td className="px-2 py-1.5">{d.status}</td>
                  <td className="max-w-[240px] truncate px-2 py-1.5 text-rose-300/80">
                    {d.error_message ?? ''}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {deployments.length === 0 ? (
            <div className="px-3 py-4 text-[12px] text-slate-600">
              No deployments yet.
            </div>
          ) : null}
        </div>
      </section>
    </div>
  )
}
