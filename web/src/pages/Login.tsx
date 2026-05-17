import { useEffect, useState } from 'react'
import { useNavigate, useLocation } from 'react-router-dom'
import { useAuth } from '../context/AuthContext'
import { ThemeToggle } from '../components/ThemeToggle'
import { shell } from '../lib/themeClasses'

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
    <div className="relative flex min-h-screen items-center justify-center bg-slate-50 px-4 dark:bg-slate-950">
      <div className="absolute right-4 top-4">
        <ThemeToggle />
      </div>
      <div className="w-full max-w-sm rounded-lg border border-slate-200 bg-white/95 p-6 dark:border-slate-800 dark:bg-slate-900/90">
        <h1 className={shell.heading}>ARX MDM</h1>
        <p className="mt-1 text-xs text-slate-600 dark:text-slate-500">
          Sign in to the operations dashboard
        </p>
        <form className="mt-6 space-y-4" onSubmit={onSubmit}>
          <div>
            <label
              className="block text-xs font-medium text-slate-600 dark:text-slate-400"
              htmlFor="user"
            >
              Username
            </label>
            <input
              id="user"
              autoComplete="username"
              className={`mt-1 text-sm outline-none focus:border-slate-400 dark:focus:border-slate-500 ${shell.inputFull}`}
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              required
            />
          </div>
          <div>
            <label
              className="block text-xs font-medium text-slate-600 dark:text-slate-400"
              htmlFor="pass"
            >
              Password
            </label>
            <input
              id="pass"
              type="password"
              autoComplete="current-password"
              className={`mt-1 text-sm outline-none focus:border-slate-400 dark:focus:border-slate-500 ${shell.inputFull}`}
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
            />
          </div>
          {err ? (
            <div className="rounded border border-rose-200 bg-rose-50 px-2 py-1.5 text-xs text-rose-800 dark:border-rose-900/60 dark:bg-rose-950/40 dark:text-rose-800 dark:text-rose-200">
              {err}
            </div>
          ) : null}
          <button
            type="submit"
            disabled={busy}
            className={`w-full py-2 text-sm font-medium ${shell.btnPrimary}`}
          >
            {busy ? 'Signing in…' : 'Sign in'}
          </button>
        </form>
      </div>
    </div>
  )
}
