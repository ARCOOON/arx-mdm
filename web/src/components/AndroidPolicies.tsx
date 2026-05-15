import { useCallback, useEffect, useState } from 'react'
import { useAuth } from '../context/AuthContext'
import { dashboardFetch } from '../lib/ticketsApi'
import { useWebSocket } from '../hooks/useWebSocket'
import type { AndroidPolicyWire } from '../types/ws'

type Props = {
  assetId: string
  humanId: string
}

function parsePolicy(raw: unknown): AndroidPolicyWire | null {
  if (!raw || typeof raw !== 'object') {
    return null
  }
  const o = raw as Record<string, unknown>
  return {
    camera_disabled: o.camera_disabled === true,
    screen_lock_timeout_ms:
      typeof o.screen_lock_timeout_ms === 'number'
        ? o.screen_lock_timeout_ms
        : Number(o.screen_lock_timeout_ms) || 0,
    wipe_requested: o.wipe_requested === true,
  }
}

export function AndroidPolicies({ assetId, humanId }: Props) {
  const { canOperate } = useAuth()
  const { subscribeServerMessages } = useWebSocket()
  const [policy, setPolicy] = useState<AndroidPolicyWire | null>(null)
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [timeoutSec, setTimeoutSec] = useState('60')

  const load = useCallback(async () => {
    setErr(null)
    const res = await dashboardFetch(
      `/v1/assets/${encodeURIComponent(assetId)}/android-policy`,
    )
    const body = await res.json().catch(() => null)
    if (!res.ok) {
      const msg =
        body && typeof body === 'object' && 'error' in body
          ? String((body as { error?: string }).error ?? res.statusText)
          : res.statusText
      setErr(msg)
      setPolicy(null)
      setLoading(false)
      return
    }
    const p = parsePolicy(body)
    setPolicy(p)
    if (p && p.screen_lock_timeout_ms > 0) {
      setTimeoutSec(String(Math.round(p.screen_lock_timeout_ms / 1000)))
    }
    setLoading(false)
  }, [assetId])

  useEffect(() => {
    void load()
  }, [load])

  useEffect(() => {
    return subscribeServerMessages((msg) => {
      if (msg.type === 'android_policy_updated' && msg.asset_id === assetId) {
        setPolicy(msg.policy)
        if (msg.policy.screen_lock_timeout_ms > 0) {
          setTimeoutSec(
            String(Math.round(msg.policy.screen_lock_timeout_ms / 1000)),
          )
        }
      }
    })
  }, [assetId, subscribeServerMessages])

  const patch = useCallback(
    async (partial: Partial<AndroidPolicyWire>) => {
      if (!canOperate) {
        return
      }
      setSaving(true)
      setErr(null)
      const res = await dashboardFetch(
        `/v1/assets/${encodeURIComponent(assetId)}/android-policy`,
        {
          method: 'PATCH',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(partial),
        },
      )
      const body = await res.json().catch(() => null)
      if (!res.ok) {
        const msg =
          body && typeof body === 'object' && 'error' in body
            ? String((body as { error?: string }).error ?? res.statusText)
            : res.statusText
        setErr(msg)
        setSaving(false)
        return
      }
      const p = parsePolicy(body)
      if (p) {
        setPolicy(p)
      }
      setSaving(false)
    },
    [assetId, canOperate],
  )

  const applyTimeout = useCallback(() => {
    const sec = Number(timeoutSec)
    if (!Number.isFinite(sec) || sec < 1) {
      setErr('Lock timeout must be at least 1 second.')
      return
    }
    void patch({ screen_lock_timeout_ms: Math.round(sec * 1000) })
  }, [patch, timeoutSec])

  const requestWipe = useCallback(() => {
    if (
      !window.confirm(
        `Request factory reset for ${humanId}? This cannot be undone on the device.`,
      )
    ) {
      return
    }
    void patch({ wipe_requested: true })
  }, [humanId, patch])

  if (loading) {
    return (
      <p className="text-[12px] text-slate-500">Loading Android policy…</p>
    )
  }

  if (!policy) {
    return (
      <p className="text-[12px] text-rose-300/90">
        {err ?? 'Could not load Android policy.'}
      </p>
    )
  }

  return (
    <div className="max-w-lg space-y-4">
      <p className="text-[12px] text-slate-500">
        Policies are delivered to the device on the next mTLS telemetry response
        (typically within the configured heartbeat interval). Dashboard
        connections receive live updates over WebSocket.
      </p>
      {err ? (
        <div className="rounded border border-rose-900/60 bg-rose-950/30 px-3 py-2 text-[12px] text-rose-200">
          {err}
        </div>
      ) : null}

      <div className="flex items-center justify-between rounded border border-slate-800 bg-slate-900/30 px-3 py-2">
        <span className="text-[12px] text-slate-200">Disable camera</span>
        <button
          type="button"
          disabled={!canOperate || saving}
          role="switch"
          aria-checked={policy.camera_disabled}
          onClick={() =>
            void patch({ camera_disabled: !policy.camera_disabled })
          }
          className={`relative h-6 w-11 rounded-full transition-colors ${
            policy.camera_disabled ? 'bg-sky-600' : 'bg-slate-700'
          } disabled:opacity-40`}
        >
          <span
            className={`absolute top-0.5 size-5 rounded-full bg-white shadow transition-transform ${
              policy.camera_disabled ? 'left-5' : 'left-0.5'
            }`}
          />
        </button>
      </div>

      <div className="rounded border border-slate-800 bg-slate-900/30 px-3 py-3">
        <div className="mb-2 text-[10px] font-semibold uppercase text-slate-500">
          Screen lock timeout (idle)
        </div>
        <div className="flex flex-wrap items-end gap-2">
          <label className="text-[11px] text-slate-400">
            Seconds
            <input
              type="number"
              min={1}
              disabled={!canOperate || saving}
              value={timeoutSec}
              onChange={(e) => setTimeoutSec(e.target.value)}
              className="ml-2 w-24 rounded border border-slate-700 bg-slate-950 px-2 py-1 text-[12px] text-slate-100"
            />
          </label>
          <button
            type="button"
            disabled={!canOperate || saving}
            onClick={applyTimeout}
            className="rounded bg-slate-700 px-3 py-1.5 text-[12px] font-medium text-slate-100 hover:bg-slate-600 disabled:opacity-40"
          >
            Apply
          </button>
        </div>
        <p className="mt-2 text-[10px] text-slate-600">
          Current server value: {policy.screen_lock_timeout_ms} ms (0 = unchanged on device).
        </p>
      </div>

      <div className="rounded border border-rose-900/50 bg-rose-950/20 px-3 py-3">
        <div className="mb-2 text-[10px] font-semibold uppercase text-rose-300/90">
          Danger zone
        </div>
        <p className="mb-3 text-[11px] text-rose-200/80">
          Remote wipe issues a factory reset command on the next device check-in.
          {policy.wipe_requested ? ' Wipe is currently requested.' : ''}
        </p>
        <button
          type="button"
          disabled={!canOperate || saving}
          onClick={requestWipe}
          className="rounded bg-rose-700 px-3 py-2 text-[12px] font-semibold text-white hover:bg-rose-600 disabled:opacity-40"
        >
          Remote wipe
        </button>
      </div>
    </div>
  )
}
