export interface User {
  id: string
  username: string
  role: 'admin' | 'user' | string
  displayName: string
  createdAt: number
  updatedAt: number
}

export interface UserSummary extends User {
  deviceCount: number
}

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
  hasOpenClaw: boolean
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
  userId?: string
}

export interface WsEvent<T = unknown> {
  event: string
  ts: number
  data: T
}

export interface BatchJobItem {
  deviceId: string
  status: 'pending' | 'running' | 'success' | 'error' | string
  commandId?: string
  error?: string
  createdAt: number
  updatedAt: number
}

export interface BatchJob {
  id: string
  command: string
  status: 'queued' | 'running' | 'success' | 'failed' | string
  createdAt: number
  updatedAt: number
  totalCount: number
  pendingCount: number
  runningCount: number
  successCount: number
  errorCount: number
  items: BatchJobItem[]
}

export interface AuditLogEntry {
  id: number
  timestamp: number
  action: string
  targetDeviceId?: string
  detail?: string
  adminIp?: string
  result: string
}

export interface AuditLogPage {
  items: AuditLogEntry[]
  total: number
  limit: number
  offset: number
}
