import { useCallback, useEffect, useRef, useState } from 'react'
import type { AgentUplinkMessage, RegistryResultMessage } from '../types/ws'

type RegistryEditorProps = {
  targetArxId: string
  sendJson: (payload: Record<string, unknown>) => void
  subscribeAgentUplink: (handler: (msg: AgentUplinkMessage) => void) => () => void
  isWindowsAsset: boolean
}

export function RegistryEditor({
  targetArxId,
  sendJson,
  subscribeAgentUplink,
  isWindowsAsset,
}: RegistryEditorProps) {
  const [keyPath, setKeyPath] = useState('HKLM\\SOFTWARE')
  const [valueName, setValueName] = useState('')
  const [valueType, setValueType] = useState('string')
  const [data, setData] = useState('')
  const [status, setStatus] = useState<string | null>(null)
  const pendingRef = useRef<string | null>(null)

  const onRegistryResult = useCallback(
    (msg: AgentUplinkMessage) => {
      if (msg.type !== 'registry_result') {
        return
      }
      const r = msg as RegistryResultMessage
      if (r.target_arx_id !== targetArxId) {
        return
      }
      if (pendingRef.current && r.request_id !== pendingRef.current) {
        return
      }
      pendingRef.current = null
      if (r.ok) {
        setStatus(r.message ?? 'ok')
        if (r.data !== undefined) {
          setData(r.data)
        }
      } else {
        setStatus(r.error ?? 'failed')
      }
    },
    [targetArxId],
  )

  useEffect(() => {
    return subscribeAgentUplink(onRegistryResult)
  }, [onRegistryResult, subscribeAgentUplink])

  const run = (action: string, extra: Record<string, unknown> = {}) => {
    const request_id = crypto.randomUUID()
    pendingRef.current = request_id
    setStatus('pending…')
    sendJson({
      action,
      target_arx_id: targetArxId,
      request_id,
      key_path: keyPath,
      value_name: valueName,
      ...extra,
    })
  }

  if (!isWindowsAsset) {
    return (
      <div className="rounded border border-slate-800 bg-slate-900/40 p-4 text-xs text-slate-500">
        Registry editor is available when the asset reports a Windows OS (native
        registry API).
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-3 rounded border border-slate-800 bg-slate-900/40 p-3">
      <div className="text-[11px] font-semibold uppercase tracking-wide text-slate-500">
        Registry
      </div>
      <label className="block text-[11px] text-slate-400">
        Key path
        <input
          className="mt-0.5 w-full rounded border border-slate-700 bg-slate-950 px-2 py-1 font-mono text-[12px] text-slate-100"
          value={keyPath}
          onChange={(e) => setKeyPath(e.target.value)}
          spellCheck={false}
        />
      </label>
      <label className="block text-[11px] text-slate-400">
        Value name (empty = default value)
        <input
          className="mt-0.5 w-full rounded border border-slate-700 bg-slate-950 px-2 py-1 font-mono text-[12px] text-slate-100"
          value={valueName}
          onChange={(e) => setValueName(e.target.value)}
          spellCheck={false}
        />
      </label>
      <div className="grid gap-2 sm:grid-cols-2">
        <label className="block text-[11px] text-slate-400">
          Type (write)
          <select
            className="mt-0.5 w-full rounded border border-slate-700 bg-slate-950 px-2 py-1 text-[12px] text-slate-100"
            value={valueType}
            onChange={(e) => setValueType(e.target.value)}
          >
            <option value="string">string</option>
            <option value="dword">dword</option>
            <option value="qword">qword</option>
            <option value="expand_string">expand_string</option>
            <option value="binary">binary (hex)</option>
            <option value="multi_string">multi_string (lines)</option>
          </select>
        </label>
      </div>
      <label className="block text-[11px] text-slate-400">
        Data
        <textarea
          className="mt-0.5 min-h-[88px] w-full rounded border border-slate-700 bg-slate-950 px-2 py-1 font-mono text-[12px] text-slate-100"
          value={data}
          onChange={(e) => setData(e.target.value)}
          spellCheck={false}
        />
      </label>
      <div className="flex flex-wrap gap-2">
        <button
          type="button"
          className="rounded border border-slate-600 bg-slate-800 px-2 py-1 text-[11px] font-medium text-slate-100 hover:bg-slate-700"
          onClick={() => run('registry_read')}
        >
          Read
        </button>
        <button
          type="button"
          className="rounded border border-sky-800/80 bg-sky-950/50 px-2 py-1 text-[11px] font-medium text-sky-100 hover:bg-sky-900/50"
          onClick={() =>
            run('registry_write', {
              type: valueType,
              data,
            })
          }
        >
          Write value
        </button>
        <button
          type="button"
          className="rounded border border-rose-800/70 bg-rose-950/30 px-2 py-1 text-[11px] font-medium text-rose-100 hover:bg-rose-900/40"
          onClick={() => {
            if (
              !window.confirm(
                'Delete this value from the registry on the endpoint?',
              )
            ) {
              return
            }
            run('registry_delete', { delete_key: false })
          }}
        >
          Delete value
        </button>
        <button
          type="button"
          className="rounded border border-rose-900/80 bg-rose-950/40 px-2 py-1 text-[11px] font-medium text-rose-100 hover:bg-rose-900/50"
          onClick={() => {
            if (
              !window.confirm(
                'Delete the subkey named by Key path (must be empty of subkeys). Continue?',
              )
            ) {
              return
            }
            run('registry_delete', { delete_key: true, value_name: '' })
          }}
        >
          Delete key
        </button>
      </div>
      {status ? (
        <p className="font-mono text-[11px] text-slate-400">{status}</p>
      ) : null}
    </div>
  )
}
