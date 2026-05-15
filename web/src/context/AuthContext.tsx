import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react'
import { clearAccessToken, getAccessToken, setAccessToken } from '../lib/session'

export type AuthUser = {
  id: string
  username: string
  role: 'admin' | 'operator' | 'viewer'
}

type AuthContextValue = {
  token: string
  user: AuthUser | null
  loading: boolean
  error: string | null
  login: (username: string, password: string) => Promise<void>
  logout: () => void
  refreshUser: () => Promise<void>
  isAdmin: boolean
  canOperate: boolean
}

const AuthContext = createContext<AuthContextValue | null>(null)

function parseUser(raw: unknown): AuthUser | null {
  if (!raw || typeof raw !== 'object') {
    return null
  }
  const o = raw as Record<string, unknown>
  const id = typeof o.id === 'string' ? o.id : ''
  const username = typeof o.username === 'string' ? o.username : ''
  const role = typeof o.role === 'string' ? o.role : ''
  if (!id || !username) {
    return null
  }
  if (role !== 'admin' && role !== 'operator' && role !== 'viewer') {
    return null
  }
  return { id, username, role }
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const [token, setToken] = useState(() => getAccessToken())
  const [user, setUser] = useState<AuthUser | null>(null)
  const [loading, setLoading] = useState(!!getAccessToken())
  const [error, setError] = useState<string | null>(null)

  const refreshUser = useCallback(async () => {
    const t = getAccessToken()
    setToken(t)
    if (!t) {
      setUser(null)
      setLoading(false)
      return
    }
    setLoading(true)
    setError(null)
    try {
      const res = await fetch('/v1/auth/me', {
        headers: { Authorization: `Bearer ${t}` },
      })
      if (!res.ok) {
        clearAccessToken()
        setToken('')
        setUser(null)
        setError('Session expired')
        return
      }
      const data = await res.json()
      const u = parseUser(data)
      setUser(u)
    } catch {
      clearAccessToken()
      setToken('')
      setUser(null)
      setError('Could not verify session')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void refreshUser()
  }, [refreshUser])

  const login = useCallback(async (username: string, password: string) => {
    setError(null)
    const res = await fetch('/v1/auth/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username, password }),
    })
    const body = (await res.json().catch(() => null)) as
      | { error?: string; token?: string; user?: unknown }
      | null
    if (!res.ok) {
      throw new Error(body?.error ?? 'Login failed')
    }
    const tok = typeof body?.token === 'string' ? body.token : ''
    if (!tok) {
      throw new Error('Invalid server response')
    }
    setAccessToken(tok)
    setToken(tok)
    const u = parseUser(body?.user)
    setUser(u)
    if (!u) {
      await refreshUser()
    }
  }, [refreshUser])

  const logout = useCallback(() => {
    clearAccessToken()
    setToken('')
    setUser(null)
    setError(null)
  }, [])

  const value = useMemo<AuthContextValue>(
    () => ({
      token,
      user,
      loading,
      error,
      login,
      logout,
      refreshUser,
      isAdmin: user?.role === 'admin',
      canOperate: user?.role === 'admin' || user?.role === 'operator',
    }),
    [token, user, loading, error, login, logout, refreshUser],
  )

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext)
  if (!ctx) {
    throw new Error('useAuth must be used within AuthProvider')
  }
  return ctx
}
