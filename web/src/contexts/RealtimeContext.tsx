import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react'
import { fetchDevices } from '../api/client'
import type { CommandRecord, DeviceSnapshot, WsEvent } from '../types'
import { useAuth } from './AuthContext'

interface RealtimeContextValue {
  devices: DeviceSnapshot[]
  loading: boolean
  wsConnected: boolean
  commandRecords: Record<string, CommandRecord>
  refreshDevices: () => Promise<void>
  getDeviceById: (id: string) => DeviceSnapshot | undefined
}

const RealtimeContext = createContext<RealtimeContextValue | undefined>(undefined)
const maxCommandRecords = 100

function sortDevices(list: DeviceSnapshot[]): DeviceSnapshot[] {
  return [...list].sort((a, b) => b.updatedAt - a.updatedAt)
}

function upsertDevice(list: DeviceSnapshot[], incoming: DeviceSnapshot): DeviceSnapshot[] {
  const index = list.findIndex((item) => item.id === incoming.id)
  if (index === -1) {
    return sortDevices([...list, incoming])
  }

  const next = [...list]
  next[index] = incoming
  return sortDevices(next)
}

function buildWsUrl(): string {
  const protocol = window.location.protocol === 'https:' ? 'wss' : 'ws'
  const host = import.meta.env.DEV ? 'localhost:18790' : window.location.host
  return `${protocol}://${host}/api/ws`
}

export function RealtimeProvider({ children }: { children: ReactNode }) {
  const { token } = useAuth()
  const [devices, setDevices] = useState<DeviceSnapshot[]>([])
  const [loading, setLoading] = useState(false)
  const [wsConnected, setWsConnected] = useState(false)
  const [commandRecords, setCommandRecords] = useState<Record<string, CommandRecord>>({})
  const reconnectTimerRef = useRef<number | null>(null)
  const reconnectAttemptsRef = useRef(0)

  const refreshDevices = useCallback(async () => {
    if (!token) {
      setDevices([])
      return
    }
    setLoading(true)
    try {
      const list = await fetchDevices(token)
      setDevices(sortDevices(list))
    } finally {
      setLoading(false)
    }
  }, [token])

  useEffect(() => {
    void refreshDevices()
  }, [refreshDevices])

  useEffect(() => {
    if (!token) {
      setWsConnected(false)
      return
    }

    let closedByApp = false
    let socket: WebSocket | null = null

    const scheduleReconnect = () => {
      if (reconnectTimerRef.current !== null) {
        window.clearTimeout(reconnectTimerRef.current)
        reconnectTimerRef.current = null
      }
      const backoffMs = Math.min(1000 * 2 ** reconnectAttemptsRef.current, 30000)
      const jitterMs = Math.floor(Math.random() * 500)
      const delayMs = Math.min(backoffMs + jitterMs, 30000)
      reconnectAttemptsRef.current += 1
      reconnectTimerRef.current = window.setTimeout(connect, delayMs)
    }

    const connect = () => {
      socket = new WebSocket(buildWsUrl())

      socket.onopen = () => {
        reconnectAttemptsRef.current = 0
        socket?.send(
          JSON.stringify({
            type: 'auth',
            data: {
              token,
            },
          }),
        )
        setWsConnected(true)
      }

      socket.onmessage = (msg) => {
        try {
          const payload = JSON.parse(msg.data) as WsEvent

          if (payload.event === 'snapshot' && Array.isArray(payload.data)) {
            setDevices(sortDevices(payload.data as DeviceSnapshot[]))
            return
          }

          if (
            payload.event === 'device_connected' ||
            payload.event === 'device_disconnected' ||
            payload.event === 'device_heartbeat'
          ) {
            setDevices((prev) => upsertDevice(prev, payload.data as DeviceSnapshot))
            return
          }

          if (payload.event === 'command_result') {
            const rec = payload.data as CommandRecord
            setCommandRecords((prev) => {
              const next = { ...prev, [rec.id]: rec }
              const ids = Object.keys(next)
              if (ids.length <= maxCommandRecords) {
                return next
              }

              let oldestID = ids[0]
              let oldestCreatedAt = next[oldestID]?.createdAt ?? Number.POSITIVE_INFINITY
              for (let index = 1; index < ids.length; index += 1) {
                const id = ids[index]
                const createdAt = next[id]?.createdAt ?? Number.POSITIVE_INFINITY
                if (createdAt < oldestCreatedAt) {
                  oldestCreatedAt = createdAt
                  oldestID = id
                }
              }
              delete next[oldestID]
              return next
            })
          }
        } catch {
          // Ignore malformed events to keep UI stable.
        }
      }

      socket.onclose = (event) => {
        setWsConnected(false)
        if (closedByApp) {
          return
        }
        if (event.code === 4003 || event.code === 1008) {
          reconnectAttemptsRef.current = 0
          return
        }
        scheduleReconnect()
      }

      socket.onerror = () => {
        socket?.close()
      }
    }

    connect()

    return () => {
      closedByApp = true
      reconnectAttemptsRef.current = 0
      setWsConnected(false)
      if (reconnectTimerRef.current !== null) {
        window.clearTimeout(reconnectTimerRef.current)
        reconnectTimerRef.current = null
      }
      socket?.close()
    }
  }, [token])

  const value = useMemo<RealtimeContextValue>(
    () => ({
      devices,
      loading,
      wsConnected,
      commandRecords,
      refreshDevices,
      getDeviceById: (id: string) => devices.find((item) => item.id === id),
    }),
    [commandRecords, devices, loading, refreshDevices, wsConnected],
  )

  return <RealtimeContext.Provider value={value}>{children}</RealtimeContext.Provider>
}

// eslint-disable-next-line react-refresh/only-export-components
export function useRealtime() {
  const ctx = useContext(RealtimeContext)
  if (!ctx) {
    throw new Error('useRealtime 必须在 RealtimeProvider 内使用')
  }
  return ctx
}
