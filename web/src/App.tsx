import { Navigate, Outlet, Route, Routes } from 'react-router-dom'
import { AppLayout } from './components/AppLayout'
import { AuthProvider, useAuth } from './contexts/AuthContext'
import { RealtimeProvider } from './contexts/RealtimeContext'
import { DashboardPage } from './pages/DashboardPage'
import { DeviceDetailPage } from './pages/DeviceDetailPage'
import { IMConfigPage } from './pages/IMConfigPage'
import { LoginPage } from './pages/LoginPage'
import { OnboardingPage } from './pages/OnboardingPage'

function RedirectIfAuthed() {
  const { token } = useAuth()
  if (token) {
    return <Navigate to="/dashboard" replace />
  }
  return <LoginPage />
}

function RequireAuth() {
  const { token } = useAuth()
  if (!token) {
    return <Navigate to="/login" replace />
  }
  return <Outlet />
}

function RealtimeLayout() {
  return (
    <RealtimeProvider>
      <AppLayout>
        <Outlet />
      </AppLayout>
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
          <Route path="/devices/:id" element={<DeviceDetailPage />} />
          <Route path="/devices/:id/im-config" element={<IMConfigPage />} />
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
