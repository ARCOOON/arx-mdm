import { useEffect, useState } from 'react'
import { useNavigate, useLocation } from 'react-router-dom'
import { useAuth } from '../context/AuthContext'

export function LoginPage() {
  const { login, user, loading } = useAuth()
  const navigate = useNavigate()
  const location = useLocation()
  const from =
    (location.state as { from?: { pathname?: string } } | null)?.from
      ?.pathname ?? '/'

  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [err, setErr] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (!loading && user) {
      navigate(from, { replace: true })
    }
  }, [loading, user, navigate, from])

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault()
    setErr(null)
    setBusy(true)
    try {
      await login(username, password)
      navigate(from, { replace: true })
    } catch (e) {
      setErr(e instanceof Error ? e.message : 'Login failed')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-slate-950 px-4">
      <div className="w-full max-w-sm rounded-lg border border-slate-800 bg-slate-900/90 p-6 shadow-xl">
        <h1 className="text-lg font-semibold text-slate-100">ARX MDM</h1>
        <p className="mt-1 text-xs text-slate-500">Sign in to the operations dashboard</p>
        <form className="mt-6 space-y-4" onSubmit={onSubmit}>
          <div>
            <label className="block text-xs font-medium text-slate-400" htmlFor="user">
              Username
            </label>
            <input
              id="user"
              autoComplete="username"
              className="mt-1 w-full rounded border border-slate-700 bg-slate-950 px-2 py-1.5 text-sm text-slate-100 outline-none focus:border-slate-500"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              required
            />
          </div>
          <div>
            <label className="block text-xs font-medium text-slate-400" htmlFor="pass">
              Password
            </label>
            <input
              id="pass"
              type="password"
              autoComplete="current-password"
              className="mt-1 w-full rounded border border-slate-700 bg-slate-950 px-2 py-1.5 text-sm text-slate-100 outline-none focus:border-slate-500"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
            />
          </div>
          {err ? (
            <div className="rounded border border-rose-900/60 bg-rose-950/40 px-2 py-1.5 text-xs text-rose-200">
              {err}
            </div>
          ) : null}
          <button
            type="submit"
            disabled={busy}
            className="w-full rounded bg-slate-100 py-2 text-sm font-medium text-slate-900 hover:bg-white disabled:opacity-50"
          >
            {busy ? 'Signing in…' : 'Sign in'}
          </button>
        </form>
      </div>
    </div>
  )
}
