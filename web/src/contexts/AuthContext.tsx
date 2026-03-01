import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { AUTH_TOKEN_STORAGE_KEY, fetchMe, login, onUnauthorized } from '../api/client'
import type { User } from '../types'

interface AuthContextValue {
  token: string | null
  user: User | null
  isAdmin: boolean
  loading: boolean
  loginWithPassword: (username: string, password: string) => Promise<void>
  refreshMe: () => Promise<void>
  logout: () => void
}

const AuthContext = createContext<AuthContextValue | undefined>(undefined)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [token, setToken] = useState<string | null>(() => {
    const stored = sessionStorage.getItem(AUTH_TOKEN_STORAGE_KEY)
    return stored && stored.trim() ? stored : null
  })
  const [user, setUser] = useState<User | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    onUnauthorized(() => {
      setToken(null)
      setUser(null)
    })
    return () => onUnauthorized(null)
  }, [])

  useEffect(() => {
    if (!token) {
      setUser(null)
      setLoading(false)
      return
    }
    setLoading(true)
    fetchMe(token)
      .then((resp) => {
        setUser(resp.user)
      })
      .catch(() => {
        sessionStorage.removeItem(AUTH_TOKEN_STORAGE_KEY)
        setToken(null)
        setUser(null)
      })
      .finally(() => {
        setLoading(false)
      })
  }, [token])

  const value = useMemo<AuthContextValue>(
    () => ({
      token,
      user,
      isAdmin: user?.role === 'admin',
      loading,
      loginWithPassword: async (username: string, password: string) => {
        const cleanUsername = username.trim()
        if (!cleanUsername || !password) {
          throw new Error('请输入用户名和密码')
        }

        setLoading(true)
        try {
          const result = await login(cleanUsername, password)
          sessionStorage.setItem(AUTH_TOKEN_STORAGE_KEY, result.token)
          setToken(result.token)
          setUser(result.user)
        } finally {
          setLoading(false)
        }
      },
      refreshMe: async () => {
        if (!token) {
          setUser(null)
          return
        }
        const resp = await fetchMe(token)
        setUser(resp.user)
      },
      logout: () => {
        sessionStorage.removeItem(AUTH_TOKEN_STORAGE_KEY)
        setToken(null)
        setUser(null)
      },
    }),
    [loading, token, user],
  )

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

// eslint-disable-next-line react-refresh/only-export-components
export function useAuth() {
  const ctx = useContext(AuthContext)
  if (!ctx) {
    throw new Error('useAuth must be used within AuthProvider')
  }
  return ctx
}
