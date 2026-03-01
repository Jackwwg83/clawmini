import { Navigate, Outlet, Route, Routes } from 'react-router-dom'
import { AppLayout } from './components/AppLayout'
import { ErrorBoundary } from './components/ErrorBoundary'
import { AuthProvider, useAuth } from './contexts/AuthContext'
import { RealtimeProvider } from './contexts/RealtimeContext'
import { AuditLogPage } from './pages/AuditLogPage'
import { DashboardPage } from './pages/DashboardPage'
import { DemoPage } from './pages/DemoPage'
import { DeviceDetailPage } from './pages/DeviceDetailPage'
import { IMChannelSelectionPage } from './pages/IMChannelSelectionPage'
import { IMConfigPage } from './pages/IMConfigPage'
import { LoginPage } from './pages/LoginPage'
import { OnboardingPage } from './pages/OnboardingPage'
import { UserDetailPage } from './pages/UserDetailPage'
import { UserManagementPage } from './pages/UserManagementPage'

function RedirectIfAuthed() {
  const { token } = useAuth()
  if (token) {
    return <Navigate to="/dashboard" replace />
  }
  return <LoginPage />
}

function RequireAuth() {
  const { token, loading } = useAuth()
  if (loading) {
    return null
  }
  if (!token) {
    return <Navigate to="/login" replace />
  }
  return <Outlet />
}

function RequireAdmin() {
  const { isAdmin, loading } = useAuth()
  if (loading) {
    return null
  }
  if (!isAdmin) {
    return <Navigate to="/dashboard" replace />
  }
  return <Outlet />
}

function RealtimeLayout() {
  return (
    <RealtimeProvider>
      <ErrorBoundary>
        <AppLayout>
          <Outlet />
        </AppLayout>
      </ErrorBoundary>
    </RealtimeProvider>
  )
}

function AppRoutes() {
  return (
    <Routes>
      <Route path="/login" element={<RedirectIfAuthed />} />

      <Route element={<RequireAuth />}>
        <Route element={<RealtimeLayout />}>
          <Route path="/" element={<Navigate to="/dashboard" replace />} />
          <Route path="/dashboard" element={<DashboardPage />} />
          <Route path="/onboarding" element={<OnboardingPage />} />
          <Route path="/im-config" element={<IMChannelSelectionPage />} />
          <Route path="/audit-log" element={<AuditLogPage />} />
          <Route path="/demo" element={<DemoPage />} />
          <Route path="/devices/:id" element={<DeviceDetailPage />} />
          <Route path="/devices/:id/im-config" element={<IMChannelSelectionPage />} />
          <Route path="/devices/:id/im-config/:platform" element={<IMConfigPage />} />

          <Route element={<RequireAdmin />}>
            <Route path="/users" element={<UserManagementPage />} />
            <Route path="/users/:id" element={<UserDetailPage />} />
          </Route>

          <Route path="*" element={<Navigate to="/dashboard" replace />} />
        </Route>
      </Route>
    </Routes>
  )
}

export default function App() {
  return (
    <AuthProvider>
      <AppRoutes />
    </AuthProvider>
  )
}
