import { NavLink, Outlet, useNavigate } from 'react-router-dom'
import {
  Package,
  LayoutDashboard,
  HardDrive,
  Ticket,
  Radio,
  BookOpen,
  Users,
  ClipboardList,
  LogOut,
  Bell,
  Timer,
} from 'lucide-react'
import { useWebSocket } from '../hooks/useWebSocket'
import { useAuth } from '../context/AuthContext'

const navCls =
  'flex items-center gap-2 rounded px-2.5 py-1.5 text-xs font-medium text-slate-300 hover:bg-slate-800 hover:text-white'
const activeNavCls =
  'bg-slate-800 text-white ring-1 ring-slate-700/80'

function statusDot(state: string) {
  switch (state) {
    case 'open':
      return 'bg-emerald-500'
    case 'connecting':
      return 'bg-amber-400 animate-pulse'
    case 'error':
      return 'bg-rose-500'
    default:
      return 'bg-slate-500'
  }
}

export function AppLayout() {
  const { connectionState, lastError } = useWebSocket()
  const { user, logout, isAdmin } = useAuth()
  const navigate = useNavigate()

  return (
    <div className="flex h-full min-h-0 bg-slate-950">
      <aside className="flex w-56 shrink-0 flex-col border-r border-slate-800 bg-slate-900/80">
        <div className="border-b border-slate-800 px-3 py-2.5">
          <div className="text-[11px] font-semibold uppercase tracking-wider text-slate-500">
            ARX MDM
          </div>
          <div className="text-sm font-semibold text-slate-100">Operations</div>
          {user ? (
            <div className="mt-1 truncate text-[10px] text-slate-500" title={user.username}>
              {user.username}
              <span className="text-slate-600"> · </span>
              <span className="font-mono text-slate-400">{user.role}</span>
            </div>
          ) : null}
        </div>
        <nav className="flex flex-1 flex-col gap-0.5 p-2">
          <NavLink
            to="/"
            end
            className={({ isActive }) =>
              `${navCls} ${isActive ? activeNavCls : ''}`
            }
          >
            <LayoutDashboard className="size-3.5 shrink-0 opacity-80" />
            Dashboard
          </NavLink>
          <NavLink
            to="/assets"
            end
            className={({ isActive }) =>
              `${navCls} ${isActive ? activeNavCls : ''}`
            }
          >
            <HardDrive className="size-3.5 shrink-0 opacity-80" />
            Assets
          </NavLink>
          <NavLink
            to="/software"
            className={({ isActive }) =>
              `${navCls} ${isActive ? activeNavCls : ''}`
            }
          >
            <Package className="size-3.5 shrink-0 opacity-80" />
            Software
          </NavLink>
          <NavLink
            to="/automations"
            className={({ isActive }) =>
              `${navCls} ${isActive ? activeNavCls : ''}`
            }
          >
            <Timer className="size-3.5 shrink-0 opacity-80" />
            Automations
          </NavLink>
          <NavLink
            to="/tickets"
            className={({ isActive }) =>
              `${navCls} ${isActive ? activeNavCls : ''}`
            }
          >
            <Ticket className="size-3.5 shrink-0 opacity-80" />
            Tickets
          </NavLink>
          <NavLink
            to="/knowledge"
            className={({ isActive }) =>
              `${navCls} ${isActive ? activeNavCls : ''}`
            }
          >
            <BookOpen className="size-3.5 shrink-0 opacity-80" />
            Knowledge
          </NavLink>
          {isAdmin ? (
            <NavLink
              to="/audit"
              className={({ isActive }) =>
                `${navCls} ${isActive ? activeNavCls : ''}`
              }
            >
              <ClipboardList className="size-3.5 shrink-0 opacity-80" />
              Audit logs
            </NavLink>
          ) : null}
          {isAdmin ? (
            <NavLink
              to="/settings"
              className={({ isActive }) =>
                `${navCls} ${isActive ? activeNavCls : ''}`
              }
            >
              <Bell className="size-3.5 shrink-0 opacity-80" />
              Settings &amp; alerts
            </NavLink>
          ) : null}
          {isAdmin ? (
            <NavLink
              to="/users"
              className={({ isActive }) =>
                `${navCls} ${isActive ? activeNavCls : ''}`
              }
            >
              <Users className="size-3.5 shrink-0 opacity-80" />
              Users
            </NavLink>
          ) : null}
        </nav>
        <div className="border-t border-slate-800 p-2.5">
          <button
            type="button"
            className="mb-2 flex w-full items-center justify-center gap-2 rounded border border-slate-700 px-2 py-1.5 text-[11px] text-slate-300 hover:bg-slate-800"
            onClick={() => {
              logout()
              navigate('/login', { replace: true })
            }}
          >
            <LogOut className="size-3.5" />
            Sign out
          </button>
          <div className="flex items-center gap-2 text-[11px] text-slate-400">
            <Radio className="size-3.5 text-slate-500" />
            <span className="font-mono uppercase">{connectionState}</span>
            <span
              className={`ml-auto size-2 rounded-full ${statusDot(connectionState)}`}
              title={lastError ?? 'live'}
            />
          </div>
          {lastError ? (
            <div className="mt-1 line-clamp-2 font-mono text-[10px] text-rose-400/90">
              {lastError}
            </div>
          ) : null}
        </div>
      </aside>
      <main className="min-w-0 flex-1 overflow-auto">
        <Outlet />
      </main>
    </div>
  )
}
