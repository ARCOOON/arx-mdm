import {
  BrowserRouter,
  Navigate,
  Outlet,
  Route,
  Routes,
  useLocation,
} from 'react-router-dom'
import { WebSocketProvider } from './hooks/useWebSocket'
import { AuthProvider, useAuth } from './context/AuthContext'
import { AppLayout } from './components/Layout/AppLayout'
import { DashboardPage } from './pages/Dashboard'
import { BackupSettingsPage } from './pages/BackupSettings'
import { AutomationsPage } from './pages/Automations'
import { AssetsPage } from './pages/Assets'
import { AssetDetailPage } from './pages/AssetDetail'
import { AppCatalogPage } from './pages/AppCatalog'
import { SoftwarePage } from './pages/Software'
import { TicketsPage } from './pages/Tickets'
import { LoginPage } from './pages/Login'
import { UsersPage } from './pages/Users'
import { KnowledgeBasePage } from './pages/KnowledgeBase'
import { AuditLogsPage } from './pages/AuditLogs'
import { SettingsPage } from './pages/Settings'

function RequireAuth() {
  const { token, user, loading } = useAuth()
  const location = useLocation()

  if (loading) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-slate-50 text-sm text-slate-600 dark:bg-slate-950 dark:text-slate-400">
        Loading…
      </div>
    )
  }
  if (!token || !user) {
    return <Navigate to="/login" replace state={{ from: location }} />
  }

  return (
    <WebSocketProvider authToken={token}>
      <Outlet />
    </WebSocketProvider>
  )
}

function AppRoutes() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route element={<RequireAuth />}>
        <Route element={<AppLayout />}>
          <Route index element={<DashboardPage />} />
          <Route path="assets" element={<AssetsPage />} />
          <Route path="assets/:humanId" element={<AssetDetailPage />} />
          <Route path="software" element={<SoftwarePage />} />
          <Route path="app-catalog" element={<AppCatalogPage />} />
          <Route path="automations" element={<AutomationsPage />} />
          <Route path="tickets" element={<TicketsPage />} />
          <Route path="knowledge" element={<KnowledgeBasePage />} />
          <Route path="users" element={<UsersPage />} />
          <Route path="audit" element={<AuditLogsPage />} />
          <Route path="settings/backups" element={<BackupSettingsPage />} />
          <Route path="settings" element={<SettingsPage />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Route>
      </Route>
    </Routes>
  )
}

export default function App() {
  return (
    <BrowserRouter>
      <AuthProvider>
        <AppRoutes />
      </AuthProvider>
    </BrowserRouter>
  )
}
