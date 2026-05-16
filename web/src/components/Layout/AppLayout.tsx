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
  Shield,
  Network,
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
      return 'bg-gray-400 dark:bg-gray-700'
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

  const navCls = ({ isActive }: { isActive: boolean }) =>
    `${shell.nav} ${isActive ? shell.navActive : ''}`

  const Item = ({
    to,
    end,
    label,
    icon: Icon,
  }: {
    to: string
    label: string
    end?: boolean
    icon: typeof LayoutDashboard
  }) => (
    <NavLink to={to} end={end} onClick={navLinkClose} className={navCls}>
      <Icon className="size-3.5 shrink-0 opacity-80" />
      {label}
    </NavLink>
  )

  return (
    <div className="flex h-full min-h-0 flex-col bg-gray-50 dark:bg-neutral-950 md:flex-row">
      <header className="flex shrink-0 items-center gap-2 border-b border-gray-200 bg-white px-3 py-2 dark:border-gray-800 dark:bg-neutral-950 md:hidden">
        <button
          type="button"
          className="rounded-xl p-1.5 text-gray-700 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-neutral-900"
          aria-label="Open navigation menu"
          aria-expanded={mobileNavOpen}
          onClick={() => setMobileNavOpen(true)}
        >
          <Menu className="size-5" />
        </button>
        <div className="min-w-0 flex-1">
          <div className="text-[11px] font-semibold uppercase tracking-wider text-gray-500 dark:text-gray-400">
            ARX MDM Surface
          </div>
          <div className="truncate text-sm font-semibold text-gray-950 dark:text-gray-50">Live console</div>
        </div>
        <div className="flex shrink-0 items-center gap-1">
          <NotificationCenter />
          <ThemeToggle />
        </div>
      </header>

      {mobileNavOpen ? (
        <button
          type="button"
          className="fixed inset-0 z-40 cursor-default bg-neutral-950/70 md:hidden"
          aria-label="Close navigation menu"
          onClick={closeMobileNav}
        />
      ) : null}

      <aside
        className={`fixed inset-y-0 left-0 z-50 flex w-[17.65rem] shrink-0 flex-col border-r border-gray-200 bg-white transition-transform duration-200 ease-out dark:border-gray-800 dark:bg-neutral-950 md:relative md:z-auto md:translate-x-0 ${
          mobileNavOpen ? 'translate-x-0' : '-translate-x-full md:translate-x-0'
        }`}
      >
        <div className="border-b border-gray-200 px-3 py-2.5 dark:border-gray-800">
          <div className="flex items-start justify-between gap-2">
            <div className="min-w-0 flex-1">
              <div className="text-[11px] font-semibold uppercase tracking-wider text-gray-500 dark:text-gray-400">
                ARX Unified Endpoint Plane
              </div>
              <div className="text-sm font-semibold text-gray-950 dark:text-gray-50">
                Dense operator desk
              </div>
              {user ? (
                <div className="mt-1 truncate text-[10px] text-gray-600 dark:text-gray-400" title={user.username}>
                  {user.username}
                  <span className="text-gray-400 dark:text-gray-600"> · </span>
                  <span className="font-mono">{user.role}</span>
                </div>
              ) : null}
            </div>
            <div className="flex shrink-0 flex-col items-end gap-1">
              <button
                type="button"
                className="rounded-xl p-1 text-gray-600 hover:bg-gray-100 md:hidden dark:text-gray-300 dark:hover:bg-neutral-900"
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
        <nav className="flex flex-1 flex-col gap-0 overflow-y-auto p-3 text-[11px]">
          <div className={`${shell.navSection}`}>Command</div>
          <Item to="/" end icon={LayoutDashboard} label="Operations overview" />

          <div className={`${shell.navSection}`}>Fleet inventory</div>
          <Item to="/assets" end icon={HardDrive} label="Devices & telemetry" />
          <Item to="/software" end icon={Package} label="Software inventory" />

          <div className={`${shell.navSection}`}>Apps & payloads</div>
          <Item to="/app-catalog" icon={Box} label="Catalog artifacts" />
          <Item to="/automations" icon={Timer} label="Automation meshes" />

          <div className={`${shell.navSection}`}>Declarative posture</div>
          <Item to="/mdm-profiles" icon={Shield} label="Configuration profiles" />
          <Item to="/device-cohorts" icon={Network} label="Device cohorts" />

          <div className={`${shell.navSection}`}>Support streams</div>
          <Item to="/service-desk" icon={Headphones} label="Field operations inbox" />
          <Item to="/knowledge" icon={BookOpen} label="Knowledge fabric" />

          {isAdmin ? (
            <>
              <div className={`${shell.navSection}`}>Platform controls</div>
              <Item to="/audit" icon={ClipboardList} label="Immutable audit trails" />
              <Item to="/settings" icon={Sliders} end label="Alerting stance" />
              <Item to="/settings/backups" icon={ArchiveRestore} label="Disaster capsules" />
              <Item to="/users" icon={Users} label="Identity plane" />
            </>
          ) : null}
        </nav>
        <div className="border-t border-gray-200 p-2.5 dark:border-gray-800">
          <button
            type="button"
            className={`mb-2 flex w-full items-center justify-center gap-2 rounded-xl px-2 py-1.5 text-[11px] ${shell.btnSecondary}`}
            onClick={() => {
              logout()
              navigate('/login', { replace: true })
            }}
          >
            <LogOut className="size-3.5" />
            Secure sign-out
          </button>
          <div className="flex items-center gap-2 text-[11px] text-gray-600 dark:text-gray-400">
            <Radio className="size-3.5 text-gray-500" />
            <span className="font-mono uppercase">{connectionState}</span>
            <span className={`ml-auto size-2 rounded-full ${statusDot(connectionState)}`} title={lastError ?? 'live'} />
          </div>
          {lastError ? (
            <div className="mt-1 line-clamp-2 font-mono text-[10px] text-rose-600 dark:text-rose-400/90">{lastError}</div>
          ) : null}
        </div>
      </aside>
      <main className="min-h-0 min-w-0 flex-1 overflow-auto bg-gray-50 dark:bg-neutral-950">
        <Outlet />
      </main>
    </div>
  )
}
