import { useCallback, useEffect, useState } from 'react'
import { NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom'
import {
  ArchiveRestore,
  Box,
  Package,
  LayoutDashboard,
  HardDrive,
  Headphones,
  Radio,
  BookOpen,
  Users,
  ClipboardList,
  LogOut,
  Sliders,
  Timer,
  Menu,
  X,
} from 'lucide-react'
import { useWebSocket } from '../../hooks/useWebSocket'
import { useAuth } from '../../context/AuthContext'
import { shell } from '../../lib/themeClasses'
import { NotificationCenter } from '../NotificationCenter'
import { ThemeToggle } from '../ThemeToggle'

function statusDot(state: string) {
  switch (state) {
    case 'open':
      return 'bg-emerald-500'
    case 'connecting':
      return 'bg-amber-400 animate-pulse'
    case 'error':
      return 'bg-rose-500'
    default:
      return 'bg-slate-400 dark:bg-slate-500'
  }
}

export function AppLayout() {
  const { connectionState, lastError } = useWebSocket()
  const { user, logout, isAdmin } = useAuth()
  const navigate = useNavigate()
  const location = useLocation()
  const [mobileNavOpen, setMobileNavOpen] = useState(false)

  const closeMobileNav = useCallback(() => setMobileNavOpen(false), [])

  useEffect(() => {
    closeMobileNav()
  }, [location.pathname, closeMobileNav])

  useEffect(() => {
    if (!mobileNavOpen) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setMobileNavOpen(false)
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [mobileNavOpen])

  useEffect(() => {
    const mq = window.matchMedia('(min-width: 768px)')
    const onMq = () => {
      if (mq.matches) setMobileNavOpen(false)
    }
    mq.addEventListener('change', onMq)
    return () => mq.removeEventListener('change', onMq)
  }, [])

  useEffect(() => {
    if (!mobileNavOpen) return
    const prev = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => {
      document.body.style.overflow = prev
    }
  }, [mobileNavOpen])

  const navLinkClose = useCallback(() => {
    if (window.matchMedia('(max-width: 767px)').matches) {
      setMobileNavOpen(false)
    }
  }, [])

  return (
    <div className="flex h-full min-h-0 flex-col bg-slate-50 dark:bg-slate-950 md:flex-row">
      <header className="flex shrink-0 items-center gap-2 border-b border-slate-200 bg-white/95 px-3 py-2 dark:border-slate-800 dark:bg-slate-900/80 md:hidden">
        <button
          type="button"
          className="rounded p-1.5 text-slate-600 hover:bg-slate-200 dark:text-slate-300 dark:hover:bg-slate-800"
          aria-label="Open navigation menu"
          aria-expanded={mobileNavOpen}
          onClick={() => setMobileNavOpen(true)}
        >
          <Menu className="size-5" />
        </button>
        <div className="min-w-0 flex-1">
          <div className="text-[11px] font-semibold uppercase tracking-wider text-slate-500">
            ARX MDM
          </div>
          <div className="truncate text-sm font-semibold text-slate-900 dark:text-slate-100">
            Operations
          </div>
        </div>
        <div className="flex shrink-0 items-center gap-1">
          <NotificationCenter />
          <ThemeToggle />
        </div>
      </header>

      {mobileNavOpen ? (
        <button
          type="button"
          className="fixed inset-0 z-40 cursor-default bg-black/50 md:hidden"
          aria-label="Close navigation menu"
          onClick={closeMobileNav}
        />
      ) : null}

      <aside
        className={`fixed inset-y-0 left-0 z-50 flex w-56 shrink-0 flex-col border-r border-slate-200 bg-white/95 transition-transform duration-200 ease-out dark:border-slate-800 dark:bg-slate-900/80 md:relative md:z-auto md:translate-x-0 ${
          mobileNavOpen ? 'translate-x-0' : '-translate-x-full md:translate-x-0'
        }`}
      >
        <div className="border-b border-slate-200 px-3 py-2.5 dark:border-slate-800">
          <div className="flex items-start justify-between gap-2">
            <div className="min-w-0 flex-1">
              <div className="text-[11px] font-semibold uppercase tracking-wider text-slate-500">
                ARX MDM
              </div>
              <div className="text-sm font-semibold text-slate-900 dark:text-slate-100">
                Operations
              </div>
              {user ? (
                <div
                  className="mt-1 truncate text-[10px] text-slate-500"
                  title={user.username}
                >
                  {user.username}
                  <span className="text-slate-400 dark:text-slate-600"> · </span>
                  <span className="font-mono text-slate-600 dark:text-slate-400">
                    {user.role}
                  </span>
                </div>
              ) : null}
            </div>
            <div className="flex shrink-0 flex-col items-end gap-1">
              <button
                type="button"
                className="rounded p-1 text-slate-600 hover:bg-slate-200 md:hidden dark:text-slate-300 dark:hover:bg-slate-800"
                aria-label="Close navigation menu"
                onClick={closeMobileNav}
              >
                <X className="size-5" />
              </button>
              <div className="hidden items-center gap-1 md:flex">
                <NotificationCenter />
                <ThemeToggle />
              </div>
            </div>
          </div>
        </div>
        <nav className="flex flex-1 flex-col gap-0.5 overflow-y-auto p-2">
          <NavLink
            to="/"
            end
            onClick={navLinkClose}
            className={({ isActive }) =>
              `${shell.nav} ${isActive ? shell.navActive : ''}`
            }
          >
            <LayoutDashboard className="size-3.5 shrink-0 opacity-80" />
            Dashboard
          </NavLink>
          <NavLink
            to="/assets"
            end
            onClick={navLinkClose}
            className={({ isActive }) =>
              `${shell.nav} ${isActive ? shell.navActive : ''}`
            }
          >
            <HardDrive className="size-3.5 shrink-0 opacity-80" />
            Assets
          </NavLink>
          <NavLink
            to="/app-catalog"
            onClick={navLinkClose}
            className={({ isActive }) =>
              `${shell.nav} ${isActive ? shell.navActive : ''}`
            }
          >
            <Box className="size-3.5 shrink-0 opacity-80" />
            App Catalog
          </NavLink>
          <NavLink
            to="/software"
            onClick={navLinkClose}
            className={({ isActive }) =>
              `${shell.nav} ${isActive ? shell.navActive : ''}`
            }
          >
            <Package className="size-3.5 shrink-0 opacity-80" />
            Software
          </NavLink>
          <NavLink
            to="/automations"
            onClick={navLinkClose}
            className={({ isActive }) =>
              `${shell.nav} ${isActive ? shell.navActive : ''}`
            }
          >
            <Timer className="size-3.5 shrink-0 opacity-80" />
            Automations
          </NavLink>
          <NavLink
            to="/service-desk"
            onClick={navLinkClose}
            className={({ isActive }) =>
              `${shell.nav} ${isActive ? shell.navActive : ''}`
            }
          >
            <Headphones className="size-3.5 shrink-0 opacity-80" />
            Service desk
          </NavLink>
          <NavLink
            to="/knowledge"
            onClick={navLinkClose}
            className={({ isActive }) =>
              `${shell.nav} ${isActive ? shell.navActive : ''}`
            }
          >
            <BookOpen className="size-3.5 shrink-0 opacity-80" />
            Knowledge
          </NavLink>
          {isAdmin ? (
            <NavLink
              to="/audit"
              onClick={navLinkClose}
              className={({ isActive }) =>
                `${shell.nav} ${isActive ? shell.navActive : ''}`
              }
            >
              <ClipboardList className="size-3.5 shrink-0 opacity-80" />
              System logs
            </NavLink>
          ) : null}
          {isAdmin ? (
            <NavLink
              to="/settings"
              end
              onClick={navLinkClose}
              className={({ isActive }) =>
                `${shell.nav} ${isActive ? shell.navActive : ''}`
              }
            >
              <Sliders className="size-3.5 shrink-0 opacity-80" />
              Settings &amp; alerts
            </NavLink>
          ) : null}
          {isAdmin ? (
            <NavLink
              to="/settings/backups"
              onClick={navLinkClose}
              className={({ isActive }) =>
                `${shell.nav} ${isActive ? shell.navActive : ''}`
              }
            >
              <ArchiveRestore className="size-3.5 shrink-0 opacity-80" />
              Backup bundles
            </NavLink>
          ) : null}
          {isAdmin ? (
            <NavLink
              to="/users"
              onClick={navLinkClose}
              className={({ isActive }) =>
                `${shell.nav} ${isActive ? shell.navActive : ''}`
              }
            >
              <Users className="size-3.5 shrink-0 opacity-80" />
              Users
            </NavLink>
          ) : null}
        </nav>
        <div className="border-t border-slate-200 p-2.5 dark:border-slate-800">
          <button
            type="button"
            className={`mb-2 flex w-full items-center justify-center gap-2 px-2 py-1.5 text-[11px] ${shell.btnSecondary}`}
            onClick={() => {
              logout()
              navigate('/login', { replace: true })
            }}
          >
            <LogOut className="size-3.5" />
            Sign out
          </button>
          <div className="flex items-center gap-2 text-[11px] text-slate-600 dark:text-slate-400">
            <Radio className="size-3.5 text-slate-500" />
            <span className="font-mono uppercase">{connectionState}</span>
            <span
              className={`ml-auto size-2 rounded-full ${statusDot(connectionState)}`}
              title={lastError ?? 'live'}
            />
          </div>
          {lastError ? (
            <div className="mt-1 line-clamp-2 font-mono text-[10px] text-rose-600 dark:text-rose-400/90">
              {lastError}
            </div>
          ) : null}
        </div>
      </aside>
      <main className="min-h-0 min-w-0 flex-1 overflow-auto">
        <Outlet />
      </main>
    </div>
  )
}
