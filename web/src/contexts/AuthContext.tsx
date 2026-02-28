import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { AUTH_TOKEN_STORAGE_KEY, login, onUnauthorized } from '../api/client'

interface AuthContextValue {
  token: string | null
  loading: boolean
  loginWithToken: (rawToken: string) => Promise<void>
  logout: () => void
}

const AuthContext = createContext<AuthContextValue | undefined>(undefined)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [token, setToken] = useState<string | null>(() => {
    const stored = localStorage.getItem(AUTH_TOKEN_STORAGE_KEY)
    return stored && stored.trim() ? stored : null
  })
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    onUnauthorized(() => {
      setToken(null)
    })
    return () => onUnauthorized(null)
  }, [])

  const value = useMemo<AuthContextValue>(
    () => ({
      token,
      loading,
      loginWithToken: async (rawToken: string) => {
        const nextToken = rawToken.trim()
        if (!nextToken) {
          throw new Error('请输入令牌')
        }

        setLoading(true)
        try {
          const result = await login(nextToken)
          if (!result.ok) {
            throw new Error('登录失败')
          }
          localStorage.setItem(AUTH_TOKEN_STORAGE_KEY, nextToken)
          setToken(nextToken)
        } finally {
          setLoading(false)
        }
      },
      logout: () => {
        localStorage.removeItem(AUTH_TOKEN_STORAGE_KEY)
        setToken(null)
      },
    }),
    [loading, token],
  )

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

// eslint-disable-next-line react-refresh/only-export-components
export function useAuth() {
  const ctx = useContext(AuthContext)
  if (!ctx) {
    throw new Error('useAuth 必须在 AuthProvider 内使用')
  }
  return ctx
}
