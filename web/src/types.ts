export interface ChannelInfo {
  name: string
  status: string
  messages?: number
  error?: string
}

export interface OpenClawInfo {
  installed: boolean
  version: string
  gatewayStatus: string
  updateAvailable?: string
  channels: ChannelInfo[]
}

export interface DeviceStatus {
  cpuUsage: number
  memTotal: number
  memUsed: number
  diskTotal: number
  diskUsed: number
  uptime: number
  openclaw: OpenClawInfo
  updatedAt: number
}

export interface DeviceSnapshot {
  id: string
  hostname: string
  os: string
  arch: string
  openclawVersion?: string
  clientVersion: string
  createdAt: number
  updatedAt: number
  lastSeenAt: number
  online: boolean
  status?: DeviceStatus
}

export interface CommandRecord {
  id: string
  deviceId: string
  command: string
  args: string[]
  timeout: number
  status: 'queued' | 'sent' | 'failed' | 'completed' | string
  exitCode?: number
  stdout?: string
  stderr?: string
  durationMs?: number
  createdAt: number
  updatedAt: number
}

export interface JoinToken {
  id: string
  label: string
  createdAt: number
  expiresAt: number
  usedAt?: number
  usedByDevice?: string
}

export interface WsEvent<T = unknown> {
  event: string
  ts: number
  data: T
}
