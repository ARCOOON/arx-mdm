import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { ArrowLeft } from 'lucide-react'
import { useWebSocket } from '../hooks/useWebSocket'
import { useAuth } from '../context/AuthContext'
import { Terminal } from '../components/Terminal'
import { RegistryEditor } from '../components/RegistryEditor'
import { FileExplorer } from '../components/FileExplorer'
import { AndroidPolicies } from '../components/AndroidPolicies'
import { DeviceCommandPanel } from '../components/DeviceCommandPanel'
import { DeviceMetricsCharts } from '../components/DeviceMetricsCharts'
import { AssetInfoSection } from '../components/AssetInfoSection'
import { formatBytesPair, formatCpu } from '../lib/format'
import type {
  NetworkInterfaceWire,
  TelemetryInstalledApp,
} from '../types/ws'

type Tab = 'overview' | 'software' | 'files' | 'system' | 'android_mdm'

function deployPayloadForInventory(
  targetArxId: string,
  app: TelemetryInstalledApp,
  operation: 'install' | 'uninstall',
) {
  const request_id =
    typeof crypto !== 'undefined' && crypto.randomUUID
      ? crypto.randomUUID()
      : String(Date.now())
  if (app.source === 'dpkg' && app.id) {
    return {
      action: 'deploy_package' as const,
      target_arx_id: targetArxId,
      operation,
      package_type: 'apt',
      name: app.id,
      version: '',
      install_cmd: '',
      request_id,
    }
  }
  return {
    action: 'deploy_package' as const,
    target_arx_id: targetArxId,
    operation,
    package_type: 'winget',
    name: `wingetname:${app.name}`,
    version: '',
    install_cmd: '',
    request_id,
  }
}

export function AssetDetailPage() {
  const { humanId = '' } = useParams<{ humanId: string }>()
  const decodedId = useMemo(() => decodeURIComponent(humanId), [humanId])
  const [tab, setTab] = useState<Tab>('overview')
  const [pkgMsg, setPkgMsg] = useState<string | null>(null)
  const [ifaces, setIfaces] = useState<NetworkInterfaceWire[]>([])
  const [netErr, setNetErr] = useState<string | null>(null)
  const [hostInput, setHostInput] = useState('')
  const [hostMsg, setHostMsg] = useState<string | null>(null)
  const netListReq = useRef<string | null>(null)
  const hostSetReq = useRef<string | null>(null)
  const {
    assets,
    sendJson,
    connectionState,
    subscribeAgentUplink,
    subscribeServerMessages,
  } = useWebSocket()
  const { canOperate } = useAuth()

  useEffect(() => {
    return subscribeAgentUplink((msg) => {
      if (msg.type === 'package_result') {
        setPkgMsg(
          msg.ok
            ? `Package ${msg.operation ?? 'op'} OK`
            : `Package error: ${msg.error ?? 'unknown'}`,
        )
      }
      if (msg.type === 'net_list_result') {
        if (
          msg.target_arx_id === decodedId &&
          msg.request_id === netListReq.current
        ) {
          if (msg.ok) {
            setIfaces(msg.interfaces ?? [])
            setNetErr(null)
          } else {
            setNetErr(msg.error ?? 'net_list failed')
          }
        }
      }
      if (msg.type === 'hostname_set_result') {
        if (
          msg.target_arx_id === decodedId &&
          msg.request_id === hostSetReq.current
        ) {
          setHostMsg(
            msg.ok
              ? `Hostname updated to ${msg.hostname ?? hostInput}`
              : (msg.error ?? 'hostname change failed'),
          )
        }
      }
    })
  }, [subscribeAgentUplink, decodedId, hostInput])

  const asset = useMemo(
    () => assets.find((a) => a.human_id === decodedId),
    [assets, decodedId],
  )

  const isWindows = useMemo(() => {
    const os = (asset?.os ?? '').toLowerCase()
    return os.includes('windows')
  }, [asset?.os])

  const isAndroid = useMemo(() => {
    const t = (asset?.os_type ?? '').toLowerCase()
    if (t === 'android') {
      return true
    }
    return (asset?.os ?? '').toLowerCase().includes('android')
  }, [asset?.os, asset?.os_type])

  useEffect(() => {
    if (tab === 'android_mdm' && asset && !isAndroid) {
      setTab('overview')
    }
  }, [tab, asset, isAndroid])

  const installed = asset?.installed_software ?? []

  useEffect(() => {
    setHostInput(asset?.hostname ?? '')
  }, [asset?.hostname])

  useEffect(() => {
    if (tab !== 'system' || !asset?.c2_connected) {
      return
    }
    setNetErr(null)
    const rid =
      typeof crypto !== 'undefined' && crypto.randomUUID
        ? crypto.randomUUID()
        : String(Date.now())
    netListReq.current = rid
    sendJson({
      action: 'net_list',
      target_arx_id: decodedId,
      request_id: rid,
    })
  }, [tab, asset?.c2_connected, decodedId, sendJson])

  const applyHostname = useCallback(() => {
    if (!asset?.c2_connected) {
      return
    }
    const rid =
      typeof crypto !== 'undefined' && crypto.randomUUID
        ? crypto.randomUUID()
        : String(Date.now())
    hostSetReq.current = rid
    setHostMsg(null)
    sendJson({
      action: 'hostname_set',
      target_arx_id: decodedId,
      request_id: rid,
      hostname: hostInput.trim(),
    })
  }, [asset?.c2_connected, decodedId, hostInput, sendJson])

  const sendInventoryAction = useCallback(
    (app: TelemetryInstalledApp, op: 'install' | 'uninstall') => {
      setPkgMsg(null)
      sendJson(deployPayloadForInventory(decodedId, app, op))
    },
    [decodedId, sendJson],
  )

  if (!decodedId) {
    return null
  }

  const tabBtn = (id: Tab, label: string) => (
    <button
      type="button"
      key={id}
      onClick={() => setTab(id)}
      className={`rounded px-2.5 py-1 text-[11px] font-medium ${
        tab === id
          ? 'bg-slate-200 text-slate-900 ring-1 ring-slate-400 dark:bg-slate-800 dark:text-white dark:ring-slate-600'
          : 'text-slate-600 hover:text-slate-800 dark:text-slate-500 dark:hover:text-slate-300'
      }`}
    >
      {label}
    </button>
  )

  return (
    <div className="min-h-full bg-slate-50 px-6 py-4 dark:bg-slate-950">
      <div className="mb-4 flex flex-wrap items-center gap-3">
        <Link
          to="/assets"
          className="inline-flex items-center gap-1 text-[12px] font-medium text-sky-400/90 hover:text-sky-300"
        >
          <ArrowLeft className="size-3.5" />
          Assets
        </Link>
        <h1 className="text-lg font-semibold tracking-tight text-slate-900 dark:text-slate-100">
          {decodedId}
        </h1>
        {asset ? (
          <span
            className={
              asset.c2_connected
                ? 'text-[11px] font-medium text-emerald-400'
                : 'text-[11px] font-medium text-slate-600'
            }
          >
            {asset.c2_connected ? 'C2 online' : 'C2 offline'}
          </span>
        ) : null}
      </div>

      {asset ? (
        <div className="mb-4 flex flex-wrap gap-1 border-b border-slate-200 dark:border-slate-800 pb-2">
          {tabBtn('overview', 'Overview')}
          {tabBtn('software', 'Installed software')}
          {tabBtn('files', 'Files')}
          {tabBtn('system', 'System config')}
          {isAndroid ? tabBtn('android_mdm', 'Android MDM') : null}
        </div>
      ) : null}

      {!asset ? (
        <p className="text-sm text-slate-500">
          This asset is not in the current catalog snapshot. It may appear after
          the next telemetry update.
        </p>
      ) : tab === 'overview' ? (
        <>
          <div className="mb-6 grid gap-3 text-[12px] text-slate-300 sm:grid-cols-2 lg:grid-cols-3">
            <div className="rounded border border-slate-200 bg-slate-100/80 dark:border-slate-800 dark:bg-slate-900/40 px-3 py-2">
              <div className="text-[10px] font-semibold uppercase text-slate-500">
                Hostname
              </div>
              <div className="font-medium text-slate-900 dark:text-slate-100">{asset.hostname || '—'}</div>
            </div>
            <div className="rounded border border-slate-200 bg-slate-100/80 dark:border-slate-800 dark:bg-slate-900/40 px-3 py-2">
              <div className="text-[10px] font-semibold uppercase text-slate-500">
                OS
              </div>
              <div>{asset.os || '—'}</div>
            </div>
            <div className="rounded border border-slate-200 bg-slate-100/80 dark:border-slate-800 dark:bg-slate-900/40 px-3 py-2">
              <div className="text-[10px] font-semibold uppercase text-slate-500">
                CPU
              </div>
              <div className="truncate">
                {formatCpu(
                  asset.cpu_model,
                  asset.cpu_logical_cores,
                  asset.cpu_usage_percent,
                )}
              </div>
            </div>
            <div className="rounded border border-slate-200 bg-slate-100/80 dark:border-slate-800 dark:bg-slate-900/40 px-3 py-2">
              <div className="text-[10px] font-semibold uppercase text-slate-500">
                RAM
              </div>
              <div>
                {formatBytesPair(asset.memory_used_bytes, asset.total_ram_bytes)}
              </div>
            </div>
          </div>

          {asset.id ? (
            <AssetInfoSection deviceId={asset.id} humanId={decodedId} />
          ) : null}

          {asset.id ? (
            <DeviceMetricsCharts
              deviceId={asset.id}
              humanId={decodedId}
              subscribeServerMessages={subscribeServerMessages}
            />
          ) : null}

          {asset.id ? (
            <div className="mb-6">
              <DeviceCommandPanel
                deviceId={asset.id}
                humanId={decodedId}
                c2Connected={asset.c2_connected}
                subscribeServerMessages={subscribeServerMessages}
              />
            </div>
          ) : null}

          {canOperate ? (
          <div className="flex flex-col gap-8 lg:flex-row">
            <div className="min-w-0 flex-1">
              <Terminal
                targetArxId={decodedId}
                connectionState={connectionState}
                sendJson={sendJson}
                subscribeAgentUplink={subscribeAgentUplink}
              />
            </div>
            <div className="w-full shrink-0 lg:w-[400px]">
              <RegistryEditor
                targetArxId={decodedId}
                sendJson={sendJson}
                subscribeAgentUplink={subscribeAgentUplink}
                isWindowsAsset={isWindows}
              />
            </div>
          </div>
          ) : (
            <p className="text-[12px] text-slate-500">
              Remote terminal and registry tools are hidden for read-only users.
            </p>
          )}
        </>
      ) : tab === 'software' ? (
        <div className="space-y-3">
          <p className="text-[12px] text-slate-500">
            Inventory is refreshed from agent telemetry (native registry / dpkg
            parsing). Actions invoke allowlisted package manager binaries on the
            endpoint (no shell).
          </p>
          {pkgMsg ? (
            <div className="rounded border border-slate-300 dark:border-slate-700 bg-slate-900/50 px-3 py-2 text-[12px] text-slate-200">
              {pkgMsg}
            </div>
          ) : null}
          <div className="overflow-x-auto rounded border border-slate-200 dark:border-slate-800">
            <table className="w-full border-collapse text-left text-[11px]">
              <thead className="bg-slate-900/80 text-slate-500">
                <tr>
                  <th className="border-b border-slate-200 dark:border-slate-800 px-2 py-2">Name</th>
                  <th className="border-b border-slate-200 dark:border-slate-800 px-2 py-2">Version</th>
                  <th className="border-b border-slate-200 dark:border-slate-800 px-2 py-2">Source</th>
                  {canOperate ? (
                  <th className="border-b border-slate-200 dark:border-slate-800 px-2 py-2">Actions</th>
                  ) : null}
                </tr>
              </thead>
              <tbody className="text-slate-300">
                {installed.map((app, i) => (
                  <tr
                    key={`${app.name}-${app.version}-${i}`}
                    className="border-b border-slate-200 dark:border-slate-800/80"
                  >
                    <td className="max-w-[220px] truncate px-2 py-1.5">{app.name}</td>
                    <td className="px-2 py-1.5 font-mono text-slate-500">
                      {app.version || '—'}
                    </td>
                    <td className="px-2 py-1.5 font-mono text-sky-300/80">
                      {app.source}
                    </td>
                    {canOperate ? (
                    <td className="whitespace-nowrap px-2 py-1.5">
                      <button
                        type="button"
                        className="mr-2 text-sky-400 hover:text-sky-300 disabled:opacity-40"
                        disabled={!asset.c2_connected}
                        onClick={() => sendInventoryAction(app, 'install')}
                      >
                        Install
                      </button>
                      <button
                        type="button"
                        className="text-rose-400 hover:text-rose-300 disabled:opacity-40"
                        disabled={!asset.c2_connected}
                        onClick={() => sendInventoryAction(app, 'uninstall')}
                      >
                        Uninstall
                      </button>
                    </td>
                    ) : null}
                  </tr>
                ))}
              </tbody>
            </table>
            {installed.length === 0 ? (
              <div className="px-3 py-6 text-[12px] text-slate-600">
                No inventory reported yet. Wait for the next agent heartbeat.
              </div>
            ) : null}
          </div>
        </div>
      ) : tab === 'files' ? (
        <FileExplorer
          assetId={asset.id ?? ''}
          humanId={decodedId}
          c2Connected={asset.c2_connected}
          sendJson={sendJson}
          subscribeAgentUplink={subscribeAgentUplink}
          allowMutations={canOperate}
        />
      ) : tab === 'android_mdm' ? (
        asset.id ? (
          <AndroidPolicies assetId={asset.id} humanId={decodedId} />
        ) : (
          <p className="text-sm text-slate-500">
            Asset id is not available yet. Wait for catalog sync.
          </p>
        )
      ) : (
        <div className="space-y-4">
          <p className="text-[12px] text-slate-500">
            Network data is read on the agent with the Go <code className="text-slate-400">net</code>{' '}
            package. Hostname changes use native OS APIs (no shell).
          </p>
          {hostMsg ? (
            <div className="rounded border border-slate-300 dark:border-slate-700 bg-slate-900/50 px-3 py-2 text-[12px] text-slate-200">
              {hostMsg}
            </div>
          ) : null}
          <div className="max-w-lg space-y-2 rounded border border-slate-200 dark:border-slate-800 bg-slate-900/30 px-3 py-3">
            <div className="text-[10px] font-semibold uppercase text-slate-500">
              Hostname
            </div>
            {canOperate ? (
            <div className="flex flex-wrap gap-2">
              <input
                type="text"
                value={hostInput}
                onChange={(e) => setHostInput(e.target.value)}
                className="min-w-[200px] flex-1 rounded border border-slate-300 bg-white dark:border-slate-700 dark:bg-slate-950 px-2 py-1.5 text-[12px] text-slate-900 dark:text-slate-100"
                placeholder="New hostname"
              />
              <button
                type="button"
                disabled={!asset.c2_connected || !hostInput.trim()}
                onClick={applyHostname}
                className="rounded bg-sky-700 px-3 py-1.5 text-[12px] font-medium text-white hover:bg-sky-600 disabled:opacity-40"
              >
                Apply
              </button>
            </div>
            ) : (
              <p className="text-[12px] text-slate-400">
                Current hostname:{' '}
                <span className="font-medium text-slate-200">{asset.hostname || '—'}</span>
              </p>
            )}
          </div>
          {netErr ? (
            <div className="rounded border border-rose-900/60 bg-rose-950/30 px-3 py-2 text-[12px] text-rose-800 dark:text-rose-200">
              {netErr}
            </div>
          ) : null}
          <div className="overflow-x-auto rounded border border-slate-200 dark:border-slate-800">
            <table className="w-full border-collapse text-left text-[11px]">
              <thead className="bg-slate-900/80 text-slate-500">
                <tr>
                  <th className="border-b border-slate-200 dark:border-slate-800 px-2 py-2">Interface</th>
                  <th className="border-b border-slate-200 dark:border-slate-800 px-2 py-2">MTU</th>
                  <th className="border-b border-slate-200 dark:border-slate-800 px-2 py-2">Flags</th>
                  <th className="border-b border-slate-200 dark:border-slate-800 px-2 py-2">Addresses</th>
                </tr>
              </thead>
              <tbody className="text-slate-300">
                {ifaces.map((iface) => (
                  <tr key={`${iface.index}-${iface.name}`} className="border-b border-slate-200 dark:border-slate-800/80">
                    <td className="px-2 py-1.5 font-mono">{iface.name}</td>
                    <td className="px-2 py-1.5">{iface.mtu}</td>
                    <td className="px-2 py-1.5 text-slate-500">
                      {[iface.up ? 'up' : 'down', iface.loopback ? 'loopback' : null]
                        .filter(Boolean)
                        .join(', ')}
                    </td>
                    <td className="max-w-[360px] px-2 py-1.5 font-mono text-[10px] text-slate-400">
                      {iface.addrs?.map((a) => a.addr).join(', ') || '—'}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
            {ifaces.length === 0 && !netErr ? (
              <div className="px-3 py-6 text-[12px] text-slate-600">
                {asset.c2_connected
                  ? 'Loading interfaces…'
                  : 'Connect the agent to load interfaces.'}
              </div>
            ) : null}
          </div>
        </div>
      )}
    </div>
  )
}
