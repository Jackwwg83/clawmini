import type { CommandRecord, DeviceSnapshot, JoinToken } from '../types'

interface ApiError {
  error?: string
}

interface LoginResponse {
  ok: boolean
}

interface DevicesResponse {
  devices: DeviceSnapshot[]
}

interface JoinTokensResponse {
  tokens: JoinToken[]
}

interface ExecRequest {
  command: string
  args: string[]
  timeout: number
}

const API_BASE = '/api'
export const AUTH_TOKEN_STORAGE_KEY = 'clawmini_admin_token'

let unauthorizedHandler: (() => void) | null = null
let isHandlingUnauthorized = false

export function onUnauthorized(handler: (() => void) | null) {
  unauthorizedHandler = handler
}

function handleUnauthorized() {
  if (isHandlingUnauthorized) {
    return
  }

  isHandlingUnauthorized = true
  localStorage.removeItem(AUTH_TOKEN_STORAGE_KEY)
  try {
    unauthorizedHandler?.()
  } finally {
    isHandlingUnauthorized = false
  }
}

async function requestJson<T>(
  path: string,
  token?: string,
  init: RequestInit = {},
): Promise<T> {
  const headers = new Headers(init.headers)
  if (!headers.has('Content-Type') && init.body) {
    headers.set('Content-Type', 'application/json')
  }
  if (token) {
    headers.set('X-Admin-Token', token)
  }

  const res = await fetch(`${API_BASE}${path}`, {
    ...init,
    headers,
  })

  const data = (await res.json().catch(() => ({}))) as T & ApiError

  if (!res.ok) {
    if (res.status === 401) {
      handleUnauthorized()
    }
    throw new Error(data.error || `请求失败（${res.status}）`)
  }

  return data as T
}

export async function login(token: string): Promise<LoginResponse> {
  return requestJson<LoginResponse>('/auth/login', undefined, {
    method: 'POST',
    body: JSON.stringify({ token }),
  })
}

export async function fetchDevices(token: string): Promise<DeviceSnapshot[]> {
  const data = await requestJson<DevicesResponse>('/devices', token)
  return data.devices || []
}

export async function fetchDeviceById(token: string, id: string): Promise<DeviceSnapshot> {
  return requestJson<DeviceSnapshot>(`/devices/${id}`, token)
}

export async function execDeviceCommand(
  token: string,
  deviceId: string,
  payload: ExecRequest,
): Promise<CommandRecord> {
  return requestJson<CommandRecord>(`/devices/${deviceId}/exec`, token, {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export async function fetchCommandById(
  token: string,
  deviceId: string,
  cmdId: string,
): Promise<CommandRecord> {
  return requestJson<CommandRecord>(`/devices/${deviceId}/exec/${cmdId}`, token)
}

export async function deleteDevice(token: string, id: string): Promise<void> {
  await requestJson<{ ok: boolean }>(`/devices/${id}`, token, {
    method: 'DELETE',
  })
}

export async function createJoinToken(
  token: string,
  label: string,
  expiresInHours: number,
): Promise<JoinToken> {
  return requestJson<JoinToken>('/join-tokens', token, {
    method: 'POST',
    body: JSON.stringify({ label, expiresInHours }),
  })
}

export async function listJoinTokens(token: string): Promise<JoinToken[]> {
  const data = await requestJson<JoinTokensResponse>('/join-tokens', token)
  return data.tokens || []
}

export async function deleteJoinToken(token: string, id: string): Promise<void> {
  await requestJson<{ ok: boolean }>(`/join-tokens/${id}`, token, {
    method: 'DELETE',
  })
}
