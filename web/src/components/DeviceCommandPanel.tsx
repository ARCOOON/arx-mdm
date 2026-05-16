import { useCallback, useEffect, useRef, useState, type RefObject } from 'react'
import {
  listDeviceCommands,
  queueDeviceCommand,
  type DeviceCommandRow,
} from '../lib/deviceCommandsApi'
import { useAuth } from '../context/AuthContext'
import type { DeviceCommandUpdateMessage, ServerMessage } from '../types/ws'

type CommandType = DeviceCommandRow['command_type']

type DeviceCommandPanelProps = {
  deviceId: string
  humanId: string
  c2Connected: boolean
  subscribeServerMessages: (handler: (msg: ServerMessage) => void) => () => void
}

function statusClass(status: DeviceCommandRow['status']): string {
  switch (status) {
    case 'completed':
      return 'text-emerald-500'
    case 'failed':
      return 'text-rose-400'
    case 'sent':
      return 'text-sky-400'
    default:
      return 'text-amber-400'
  }
}

function formatTime(iso: string | undefined | null): string {
  if (!iso) {
    return '—'
  }
  try {
    return new Date(iso).toLocaleString()
  } catch {
    return iso
  }
}

export function DeviceCommandPanel({
  deviceId,
  humanId,
  c2Connected,
  subscribeServerMessages,
}: DeviceCommandPanelProps) {
  const { canOperate } = useAuth()
  const [commands, setCommands] = useState<DeviceCommandRow[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [successFlash, setSuccessFlash] = useState<string | null>(null)
  const [commandType, setCommandType] = useState<CommandType>('ping')
  const [scriptBody, setScriptBody] = useState('echo "hello from ARX"')
  const [submitting, setSubmitting] = useState(false)
  const logEndRef = useRef<HTMLDivElement | null>(null)

  const refresh = useCallback(async () => {
    if (!deviceId) {
      return
    }
    setLoading(true)
    setError(null)
    try {
      const rows = await listDeviceCommands(deviceId)
      setCommands(rows)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load command history')
    } finally {
      setLoading(false)
    }
  }, [deviceId])

  useEffect(() => {
    void refresh()
  }, [refresh])

  useEffect(() => {
    return subscribeServerMessages((msg) => {
      if (msg.type !== 'device_command_update') {
        return
      }
      const update = msg as DeviceCommandUpdateMessage
      if (update.command.target_arx_id !== humanId) {
        return
      }
      const updated = update.command
      const row: DeviceCommandRow = {
        id: updated.id,
        device_id: updated.device_id,
        command_type: updated.command_type as CommandType,
        payload: updated.payload,
        status: updated.status as DeviceCommandRow['status'],
        output: updated.output,
        created_at: updated.created_at,
        completed_at: updated.completed_at ?? null,
      }
      if (row.status === 'completed') {
        setSuccessFlash(
          `${row.command_type} command completed successfully.`,
        )
        window.setTimeout(() => setSuccessFlash(null), 8000)
      }
      setCommands((prev) => {
        const idx = prev.findIndex((c) => c.id === row.id)
        if (idx >= 0) {
          const next = [...prev]
          next[idx] = row
          return next
        }
        return [row, ...prev]
      })
    })
  }, [humanId, subscribeServerMessages])

  useEffect(() => {
    logEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [commands])

  const runCommand = async () => {
    if (!deviceId || !c2Connected) {
      return
    }
    setSubmitting(true)
    setError(null)
    try {
      const payload = commandType === 'script' ? scriptBody : undefined
      const created = await queueDeviceCommand(deviceId, commandType, payload)
      setCommands((prev) => {
        const without = prev.filter((c) => c.id !== created.id)
        return [created, ...without]
      })
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to queue command')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <section className="space-y-3 rounded border border-slate-200 bg-slate-100/80 px-3 py-3 dark:border-slate-800 dark:bg-slate-900/40">
      <PanelHeader humanId={humanId} c2Connected={c2Connected} readOnly={!canOperate} />
      {successFlash ? (
        <div
          role="alert"
          className="rounded border border-emerald-700/60 bg-emerald-950/25 px-3 py-2 text-[12px] text-emerald-100"
        >
          {successFlash}
        </div>
      ) : null}
      {canOperate ? (
      <CommandForm
        commandType={commandType}
        setCommandType={setCommandType}
        scriptBody={scriptBody}
        setScriptBody={setScriptBody}
        c2Connected={c2Connected}
        submitting={submitting}
        onRun={() => void runCommand()}
      />
      ) : (
        <p className="text-[11px] text-slate-500">
          Your account is read-only. Command execution requires an operator or admin role.
        </p>
      )}
      {error ? <ErrorBanner message={error} /> : null}
      <CommandLogView commands={commands} loading={loading} logEndRef={logEndRef} />
    </section>
  )
}

function PanelHeader({
  humanId,
  c2Connected,
  readOnly,
}: {
  humanId: string
  c2Connected: boolean
  readOnly?: boolean
}) {
  return (
    <div className="flex flex-wrap items-start justify-between gap-2">
      <div>
        <h2 className="text-[12px] font-semibold uppercase tracking-wide text-slate-500">
          {readOnly ? 'Commands (read-only)' : 'Run command'}
        </h2>
        <p className="text-[11px] text-slate-500">
          {readOnly
            ? `Command history for ${humanId}.`
            : `Queue remote instructions for ${humanId}. Commands run on the agent over the C2 WebSocket channel.`}
        </p>
      </div>
      <span
        className={
          c2Connected
            ? 'text-[11px] font-medium text-emerald-400'
            : 'text-[11px] font-medium text-slate-500'
        }
      >
        {c2Connected ? 'Agent online' : 'Agent offline'}
      </span>
    </div>
  )
}

function CommandForm({
  commandType,
  setCommandType,
  scriptBody,
  setScriptBody,
  c2Connected,
  submitting,
  onRun,
}: {
  commandType: CommandType
  setCommandType: (t: CommandType) => void
  scriptBody: string
  setScriptBody: (s: string) => void
  c2Connected: boolean
  submitting: boolean
  onRun: () => void
}) {
  return (
    <div className="flex flex-wrap items-end gap-2">
      <label className="flex flex-col gap-1">
        <span className="text-[10px] font-semibold uppercase text-slate-500">Type</span>
        <select
          value={commandType}
          onChange={(e) => setCommandType(e.target.value as CommandType)}
          className="rounded border border-slate-300 bg-white px-2 py-1.5 text-[12px] text-slate-900 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100"
        >
          <option value="ping">ping</option>
          <option value="reboot">reboot</option>
          <option value="script">script</option>
        </select>
      </label>
      {commandType === 'script' ? (
        <label className="flex min-w-[min(100%,320px)] flex-1 flex-col gap-1">
          <span className="text-[10px] font-semibold uppercase text-slate-500">
            Script
          </span>
          <textarea
            value={scriptBody}
            onChange={(e) => setScriptBody(e.target.value)}
            rows={3}
            className="w-full resize-y rounded border border-slate-300 bg-white px-2 py-1.5 font-mono text-[11px] text-slate-900 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100"
            placeholder="Shell script body"
          />
        </label>
      ) : null}
      <button
        type="button"
        disabled={!c2Connected || submitting}
        onClick={onRun}
        className="rounded bg-sky-700 px-3 py-1.5 text-[12px] font-medium text-white hover:bg-sky-600 disabled:opacity-40"
      >
        {submitting ? 'Sending…' : 'Run'}
      </button>
    </div>
  )
}

function ErrorBanner({ message }: { message: string }) {
  return (
    <div className="rounded border border-rose-900/60 bg-rose-950/30 px-3 py-2 text-[12px] text-rose-800 dark:text-rose-200">
      {message}
    </div>
  )
}

function CommandLogView({
  commands,
  loading,
  logEndRef,
}: {
  commands: DeviceCommandRow[]
  loading: boolean
  logEndRef: RefObject<HTMLDivElement | null>
}) {
  return (
    <div className="rounded border border-slate-200 bg-slate-950/90 dark:border-slate-800">
      <div className="border-b border-slate-800 px-2 py-1.5 text-[10px] font-semibold uppercase tracking-wide text-slate-500">
        Command log
      </div>
      <div className="max-h-[320px] overflow-y-auto p-2 font-mono text-[11px] leading-relaxed">
        {loading && commands.length === 0 ? (
          <p className="text-slate-500">Loading history…</p>
        ) : null}
        {!loading && commands.length === 0 ? (
          <p className="text-slate-600">No commands yet.</p>
        ) : null}
        {commands.map((cmd) => (
          <CommandLogEntry key={cmd.id} cmd={cmd} />
        ))}
        <div ref={logEndRef} />
      </div>
    </div>
  )
}

function CommandLogEntry({ cmd }: { cmd: DeviceCommandRow }) {
  return (
    <div className="mb-3 border-b border-slate-800/80 pb-3 last:mb-0 last:border-0 last:pb-0">
      <div className="flex flex-wrap items-center gap-x-2 gap-y-0.5 text-slate-300">
        <span className="text-slate-500">{formatTime(cmd.created_at)}</span>
        <span className="text-sky-300/90">{cmd.command_type}</span>
        <span className={statusClass(cmd.status)}>{cmd.status}</span>
        {cmd.command_type === 'script' && cmd.payload ? (
          <span className="truncate text-slate-600">
            ({cmd.payload.length} chars)
          </span>
        ) : null}
      </div>
      {cmd.output ? (
        <pre className="mt-1 whitespace-pre-wrap break-words text-slate-400">
          {cmd.output}
        </pre>
      ) : cmd.status === 'sent' || cmd.status === 'pending' ? (
        <p className="mt-1 text-slate-600">Awaiting agent response…</p>
      ) : null}
    </div>
  )
}
