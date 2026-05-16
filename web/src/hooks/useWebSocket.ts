import {
  createContext,
  createElement,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react'
import type {
  AgentUplinkMessage,
  AssetRow,
  CommandResultMessage,
  ServerMessage,
  ShutdownCommand,
  IncidentRow,
} from '../types/ws'

export type ConnectionState =
  | 'idle'
  | 'connecting'
  | 'open'
  | 'closed'
  | 'error'

export type DashboardWebSocketValue = {
  connectionState: ConnectionState
  lastError: string | null
  assets: AssetRow[]
  incidents: IncidentRow[]
  lastCommandResult: CommandResultMessage | null
  sendJson: (payload: ShutdownCommand | Record<string, unknown>) => void
  subscribeAgentUplink: (
    handler: (msg: AgentUplinkMessage) => void,
  ) => () => void
  subscribeServerMessages: (handler: (msg: ServerMessage) => void) => () => void
}

const DashboardWebSocketContext = createContext<DashboardWebSocketValue | null>(
  null,
)

function dashboardWebSocketURL(token: string): string {
  const qs = token ? `?token=${encodeURIComponent(token)}` : ''
  const { protocol, host } = window.location
  const wsProto = protocol === 'https:' ? 'wss:' : 'ws:'
  return `${wsProto}//${host}/v1/ws${qs}`
}

function tryParseAgentUplink(raw: unknown): AgentUplinkMessage | null {
  if (!raw || typeof raw !== 'object') {
    return null
  }
  const o = raw as { type?: string }
  switch (o.type) {
    case 'registry_result':
    case 'pty_output':
    case 'pty_exit':
    case 'pty_started':
    case 'package_result':
    case 'fs_listdir_result':
    case 'net_list_result':
    case 'hostname_set_result':
    case 'install_app_result':
      return o as AgentUplinkMessage
    default:
      return null
  }
}

function parseServerMessage(raw: unknown): ServerMessage | null {
  if (!raw || typeof raw !== 'object') {
    return null
  }
  const o = raw as { type?: string }
  switch (o.type) {
    case 'asset_snapshot':
    case 'incident_snapshot':
    case 'telemetry_update':
    case 'command_result':
    case 'android_policy_updated':
    case 'device_command_update':
      return o as ServerMessage
    default:
      return null
  }
}

function useDashboardWebSocketState(authToken: string): DashboardWebSocketValue {
  const [connectionState, setConnectionState] =
    useState<ConnectionState>('idle')
  const [lastError, setLastError] = useState<string | null>(null)
  const [assetsByID, setAssetsByID] = useState<Map<string, AssetRow>>(
    () => new Map(),
  )
  const [incidents, setIncidents] = useState<IncidentRow[]>([])
  const [lastCommandResult, setLastCommandResult] =
    useState<CommandResultMessage | null>(null)

  const wsRef = useRef<WebSocket | null>(null)
  const reconnectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const backoffRef = useRef(1500)

  const assets = useMemo(
    () =>
      [...assetsByID.values()].sort((a, b) =>
        a.human_id.localeCompare(b.human_id),
      ),
    [assetsByID],
  )

  const uplinkHandlersRef = useRef(new Set<(msg: AgentUplinkMessage) => void>())
  const serverMsgHandlersRef = useRef(new Set<(msg: ServerMessage) => void>())

  const subscribeAgentUplink = useCallback(
    (handler: (msg: AgentUplinkMessage) => void) => {
      uplinkHandlersRef.current.add(handler)
      return () => {
        uplinkHandlersRef.current.delete(handler)
      }
    },
    [],
  )

  const subscribeServerMessages = useCallback(
    (handler: (msg: ServerMessage) => void) => {
      serverMsgHandlersRef.current.add(handler)
      return () => {
        serverMsgHandlersRef.current.delete(handler)
      }
    },
    [],
  )

  const sendJson = useCallback((payload: ShutdownCommand | Record<string, unknown>) => {
    const ws = wsRef.current
    if (!ws || ws.readyState !== WebSocket.OPEN) {
      setLastCommandResult({
        type: 'command_result',
        ok: false,
        message: 'WebSocket is not connected',
      })
      return
    }
    ws.send(JSON.stringify(payload))
  }, [])

  useEffect(() => {
    let cancelled = false

    const clearReconnect = () => {
      if (reconnectTimerRef.current) {
        clearTimeout(reconnectTimerRef.current)
        reconnectTimerRef.current = null
      }
    }

    const scheduleReconnect = () => {
      clearReconnect()
      const delay = Math.min(backoffRef.current, 30_000)
      reconnectTimerRef.current = setTimeout(() => {
        backoffRef.current = Math.min(backoffRef.current * 2, 30_000)
        connect()
      }, delay)
    }

    const connect = () => {
      if (cancelled) {
        return
      }
      if (!authToken) {
        setConnectionState('idle')
        setLastError(null)
        return
      }
      clearReconnect()
      setConnectionState('connecting')
      setLastError(null)

      const url = dashboardWebSocketURL(authToken)
      let ws: WebSocket
      try {
        ws = new WebSocket(url, ['arx-dashboard'])
      } catch (e) {
        setConnectionState('error')
        setLastError(e instanceof Error ? e.message : 'WebSocket construct failed')
        scheduleReconnect()
        return
      }
      wsRef.current = ws

      ws.onopen = () => {
        if (cancelled) {
          return
        }
        backoffRef.current = 1500
        setConnectionState('open')
      }

      ws.onmessage = (ev) => {
        if (cancelled) {
          return
        }
        let parsed: unknown
        try {
          parsed = JSON.parse(String(ev.data))
        } catch {
          return
        }
        const uplink = tryParseAgentUplink(parsed)
        if (uplink) {
          uplinkHandlersRef.current.forEach((fn) => {
            fn(uplink)
          })
        }
        const msg = parseServerMessage(parsed)
        if (msg) {
          serverMsgHandlersRef.current.forEach((fn) => {
            fn(msg)
          })
        }
        if (!msg) {
          return
        }
        switch (msg.type) {
          case 'asset_snapshot': {
            const next = new Map<string, AssetRow>()
            for (const a of msg.assets ?? []) {
              next.set(a.human_id, a)
            }
            setAssetsByID(next)
            break
          }
          case 'incident_snapshot':
            setIncidents(msg.incidents ?? [])
            break
          case 'telemetry_update':
            setAssetsByID((prev) => {
              const next = new Map(prev)
              const cur = next.get(msg.asset.human_id)
              next.set(msg.asset.human_id, {
                ...msg.asset,
                id: msg.asset.id || cur?.id,
              })
              return next
            })
            break
          case 'command_result':
            setLastCommandResult(msg)
            break
          case 'android_policy_updated':
            break
          default:
            break
        }
      }

      ws.onerror = () => {
        if (cancelled) {
          return
        }
        setLastError('WebSocket transport error')
        setConnectionState('error')
      }

      ws.onclose = () => {
        wsRef.current = null
        if (cancelled) {
          return
        }
        setConnectionState('closed')
        scheduleReconnect()
      }
    }

    connect()

    return () => {
      cancelled = true
      clearReconnect()
      if (wsRef.current) {
        wsRef.current.onclose = null
        wsRef.current.close()
        wsRef.current = null
      }
    }
  }, [authToken])

  return {
    connectionState,
    lastError,
    assets,
    incidents,
    lastCommandResult,
    sendJson,
    subscribeAgentUplink,
    subscribeServerMessages,
  }
}

export function WebSocketProvider({
  children,
  authToken,
}: {
  children: ReactNode
  authToken: string
}) {
  const value = useDashboardWebSocketState(authToken)
  return createElement(
    DashboardWebSocketContext.Provider,
    { value },
    children,
  )
}

export function useWebSocket(): DashboardWebSocketValue {
  const ctx = useContext(DashboardWebSocketContext)
  if (!ctx) {
    throw new Error('useWebSocket must be used within WebSocketProvider')
  }
  return ctx
}
