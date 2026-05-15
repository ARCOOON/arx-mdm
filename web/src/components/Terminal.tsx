import { useEffect, useRef } from 'react'
import { useTheme } from '../contexts/ThemeContext'
import { FitAddon } from '@xterm/addon-fit'
import { Terminal as XTerm } from '@xterm/xterm'
import '@xterm/xterm/css/xterm.css'
import type { AgentUplinkMessage } from '../types/ws'
import type { ConnectionState } from '../hooks/useWebSocket'

function utf8ToBase64(s: string): string {
  const bytes = new TextEncoder().encode(s)
  let bin = ''
  for (const b of bytes) {
    bin += String.fromCharCode(b)
  }
  return btoa(bin)
}

function base64ToUtf8(b64: string): string {
  const bin = atob(b64)
  const bytes = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) {
    bytes[i] = bin.charCodeAt(i)
  }
  return new TextDecoder('utf-8', { fatal: false }).decode(bytes)
}

type TerminalProps = {
  targetArxId: string
  connectionState: ConnectionState
  sendJson: (payload: Record<string, unknown>) => void
  subscribeAgentUplink: (handler: (msg: AgentUplinkMessage) => void) => () => void
}

export function Terminal({
  targetArxId,
  connectionState,
  sendJson,
  subscribeAgentUplink,
}: TerminalProps) {
  const { theme } = useTheme()
  const containerRef = useRef<HTMLDivElement | null>(null)
  const termRef = useRef<XTerm | null>(null)
  const fitRef = useRef<FitAddon | null>(null)
  const requestIdRef = useRef<string | null>(null)

  useEffect(() => {
    if (connectionState !== 'open') {
      return
    }

    const el = containerRef.current
    if (!el) {
      return
    }

    const isDark = theme === 'dark'
    const term = new XTerm({
      cursorBlink: true,
      fontSize: 13,
      fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace',
      theme: {
        background: isDark ? '#0f172a' : '#f8fafc',
        foreground: isDark ? '#e2e8f0' : '#1e293b',
        cursor: '#38bdf8',
      },
    })
    const fit = new FitAddon()
    term.loadAddon(fit)
    term.open(el)
    fit.fit()
    termRef.current = term
    fitRef.current = fit

    const requestId = crypto.randomUUID()
    requestIdRef.current = requestId
    const dims = fit.proposeDimensions()
    sendJson({
      action: 'pty_start',
      target_arx_id: targetArxId,
      request_id: requestId,
      cols: dims?.cols ?? 80,
      rows: dims?.rows ?? 24,
    })

    const unsub = subscribeAgentUplink((msg) => {
      if (msg.target_arx_id !== targetArxId) {
        return
      }
      if ('request_id' in msg && msg.request_id && msg.request_id !== requestId) {
        return
      }
      if (msg.type === 'pty_output') {
        term.write(base64ToUtf8(msg.data_b64))
      } else if (msg.type === 'pty_exit') {
        term.writeln('\r\n\x1b[33m[remote session ended]\x1b[0m')
      } else if (msg.type === 'pty_started' && !msg.ok && msg.error) {
        term.writeln(`\r\n\x1b[31m${msg.error}\x1b[0m`)
      }
    })

    const d = term.onData((data) => {
      sendJson({
        action: 'pty_data',
        target_arx_id: targetArxId,
        data_b64: utf8ToBase64(data),
      })
    })

    const ro = new ResizeObserver(() => {
      fit.fit()
      const next = fit.proposeDimensions()
      if (next) {
        sendJson({
          action: 'pty_resize',
          target_arx_id: targetArxId,
          cols: next.cols,
          rows: next.rows,
        })
      }
    })
    ro.observe(el)

    return () => {
      ro.disconnect()
      d.dispose()
      unsub()
      sendJson({ action: 'pty_close', target_arx_id: targetArxId })
      term.dispose()
      termRef.current = null
      fitRef.current = null
      requestIdRef.current = null
    }
  }, [connectionState, sendJson, subscribeAgentUplink, targetArxId, theme])

  return (
    <div className="flex min-h-[320px] flex-col gap-2">
      <div className="text-[11px] font-semibold uppercase tracking-wide text-slate-500">
        Remote terminal
      </div>
      <div
        ref={containerRef}
        className="min-h-[300px] flex-1 overflow-hidden rounded border border-slate-200 bg-slate-50 p-1 dark:border-slate-800 dark:bg-slate-950"
      />
    </div>
  )
}
