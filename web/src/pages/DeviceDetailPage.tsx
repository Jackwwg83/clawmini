import {
  ArrowLeftOutlined,
  CheckCircleFilled,
  CloseCircleFilled,
  DeleteOutlined,
  ExclamationCircleFilled,
  PlayCircleOutlined,
  PoweroffOutlined,
  ReloadOutlined,
  SettingOutlined,
} from '@ant-design/icons'
import {
  Alert,
  Button,
  Card,
  Col,
  Empty,
  List,
  Modal,
  Progress,
  Row,
  Skeleton,
  Space,
  Switch,
  Tag,
  Typography,
  message,
} from 'antd'
import { useCallback, useEffect, useMemo, useRef, useState, type CSSProperties } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import {
  deleteDevice,
  execDeviceCommand,
  fetchCommandById,
  fetchDeviceById,
  fetchInstallOpenClawJob,
  installOpenClaw,
  type ConfigureIMJob,
} from '../api/client'
import { DeviceOnlineTag } from '../components/DeviceOnlineTag'
import { useAuth } from '../contexts/AuthContext'
import { useRealtime } from '../contexts/RealtimeContext'
import type { ChannelInfo, CommandRecord, DeviceSnapshot } from '../types'
import { formatDateTime, toProgress } from '../utils/format'

const TERMINAL_STATUS = new Set(['completed', 'failed'])
const COMMAND_POLL_INTERVAL_MS = 1200
const GATEWAY_STATUS_POLL_INTERVAL_MS = 15000
const LOG_TAIL_INTERVAL_MS = 6000
const INSTALL_JOB_POLL_INTERVAL_MS = 1200

type GatewayAction = 'start' | 'stop' | 'restart'
type DoctorCheckStatus = 'success' | 'warning' | 'error'
type RunDeviceCommand = (args: string[], timeout: number) => Promise<CommandRecord>

interface DoctorCheckItem {
  key: string
  title: string
  status: DoctorCheckStatus
  detail?: string
}

interface GatewayControlCardProps {
  device: DeviceSnapshot
  onRunCommand: RunDeviceCommand
  onRefreshDevice: () => Promise<void>
}

interface OpenClawUpdateCardProps {
  device: DeviceSnapshot
  onRunCommand: RunDeviceCommand
  onRefreshDevice: () => Promise<void>
}

interface OpenClawInstallCardProps {
  token: string | null
  device: DeviceSnapshot
  onRefreshDevice: () => Promise<void>
}

interface DoctorDiagnosticsCardProps {
  device: DeviceSnapshot
  onRunCommand: RunDeviceCommand
}

interface SystemResourcesCardProps {
  device: DeviceSnapshot
}

interface ChannelsStatusCardProps {
  device: DeviceSnapshot
  onRunCommand: RunDeviceCommand
  onConfigure: () => void
}

interface LogViewerCardProps {
  device: DeviceSnapshot
  onRunCommand: RunDeviceCommand
}

function isTerminalStatus(status?: string): boolean {
  if (!status) {
    return false
  }
  return TERMINAL_STATUS.has(status)
}

function waitFor(ms: number): Promise<void> {
  return new Promise((resolve) => {
    window.setTimeout(resolve, ms)
  })
}

function getErrorMessage(err: unknown, fallback: string): string {
  if (err instanceof Error && err.message) {
    return err.message
  }
  return fallback
}

function formatBytesToGb(value?: number): string {
  if (!value || value <= 0) {
    return '0.00 GB'
  }
  return `${(value / 1024 ** 3).toFixed(2)} GB`
}

function formatUptimeCn(seconds?: number): string {
  if (!seconds || seconds <= 0) {
    return '--'
  }

  const day = Math.floor(seconds / 86400)
  const hour = Math.floor((seconds % 86400) / 3600)
  const minute = Math.floor((seconds % 3600) / 60)

  if (day > 0) {
    return `${day}天${hour}小时`
  }
  if (hour > 0) {
    return `${hour}小时${minute}分钟`
  }
  return `${Math.max(1, minute)}分钟`
}

function normalizeLabel(value: string): string {
  return value.replaceAll('_', ' ').replaceAll('-', ' ').trim() || value
}

function toLowerText(value?: string): string {
  return (value || '').trim().toLowerCase()
}

function gatewayStatusMeta(status?: string): { color: string; text: string } {
  const normalized = toLowerText(status)

  if (
    ['healthy', 'running', 'active', 'started', 'ok', 'connected', 'up'].some((keyword) =>
      normalized.includes(keyword),
    )
  ) {
    return { color: 'success', text: '运行中' }
  }

  if (['starting', 'stopping', 'restart', 'reloading'].some((keyword) => normalized.includes(keyword))) {
    return { color: 'processing', text: '处理中' }
  }

  if (
    ['stopped', 'inactive', 'shutdown', 'down', 'disabled'].some((keyword) =>
      normalized.includes(keyword),
    )
  ) {
    return { color: 'default', text: '已停止' }
  }

  if (
    ['error', 'fail', 'failed', 'crash', 'unhealthy', 'critical'].some((keyword) =>
      normalized.includes(keyword),
    )
  ) {
    return { color: 'error', text: '异常' }
  }

  if (!status) {
    return { color: 'default', text: '未知' }
  }

  return { color: 'default', text: status }
}

function channelStatusMeta(status?: string): { color: string; text: string } {
  const normalized = toLowerText(status)

  if (
    ['connected', 'ok', 'healthy', 'ready', 'running'].some((keyword) => normalized.includes(keyword))
  ) {
    return { color: 'success', text: '已连接' }
  }

  if (['error', 'failed', 'fail', 'timeout', 'unhealthy'].some((keyword) => normalized.includes(keyword))) {
    return { color: 'error', text: '异常' }
  }

  if (['disconnected', 'offline', 'stop', 'inactive'].some((keyword) => normalized.includes(keyword))) {
    return { color: 'default', text: '未连接' }
  }

  if (!status) {
    return { color: 'default', text: '未知' }
  }

  return { color: 'default', text: status }
}

function commandStatusTag(status?: string) {
  if (!status) {
    return <Tag>未知</Tag>
  }

  if (status === 'completed') {
    return <Tag color="success">已完成</Tag>
  }

  if (status === 'failed') {
    return <Tag color="error">失败</Tag>
  }

  if (status === 'queued') {
    return <Tag color="warning">排队中</Tag>
  }

  if (status === 'sent') {
    return <Tag color="processing">执行中</Tag>
  }

  return <Tag>{status}</Tag>
}

function installStepStatusTag(status?: string) {
  if (status === 'success') {
    return <Tag color="success">成功</Tag>
  }
  if (status === 'failed') {
    return <Tag color="error">失败</Tag>
  }
  if (status === 'running') {
    return <Tag color="processing">执行中</Tag>
  }
  if (status === 'skipped') {
    return <Tag color="default">已跳过</Tag>
  }
  return <Tag>待执行</Tag>
}

function isCommandFailed(record: CommandRecord): boolean {
  if (record.status === 'failed') {
    return true
  }
  if (record.exitCode !== undefined && record.exitCode !== 0) {
    return true
  }
  return false
}

function parseJsonOutput(text?: string): unknown | null {
  if (!text) {
    return null
  }

  const trimmed = text.trim()
  if (!trimmed) {
    return null
  }

  try {
    return JSON.parse(trimmed) as unknown
  } catch {
    return null
  }
}

function toObject(value: unknown): Record<string, unknown> | null {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    return null
  }
  return value as Record<string, unknown>
}

function toDoctorStatus(value: unknown): DoctorCheckStatus | null {
  if (typeof value === 'boolean') {
    return value ? 'success' : 'error'
  }

  if (typeof value === 'number') {
    if (value === 0) {
      return 'success'
    }
    if (value > 0) {
      return 'warning'
    }
    return null
  }

  if (typeof value !== 'string') {
    return null
  }

  const normalized = toLowerText(value)
  if (!normalized) {
    return null
  }

  if (
    ['ok', 'pass', 'passed', 'success', 'healthy', 'ready', 'normal', 'connected'].some((keyword) =>
      normalized.includes(keyword),
    )
  ) {
    return 'success'
  }

  if (['warn', 'warning', 'degraded', 'unknown', 'pending'].some((keyword) => normalized.includes(keyword))) {
    return 'warning'
  }

  if (
    ['error', 'fail', 'failed', 'critical', 'down', 'broken', 'unhealthy', 'timeout'].some((keyword) =>
      normalized.includes(keyword),
    )
  ) {
    return 'error'
  }

  return null
}

function firstString(
  obj: Record<string, unknown>,
  keys: string[],
): string | undefined {
  for (const key of keys) {
    const value = obj[key]
    if (typeof value === 'string' && value.trim()) {
      return value.trim()
    }
  }
  return undefined
}

function extractDoctorChecks(payload: unknown): DoctorCheckItem[] {
  const items: DoctorCheckItem[] = []

  const visit = (node: unknown, path: string[]) => {
    if (Array.isArray(node)) {
      node.forEach((entry, index) => {
        visit(entry, [...path, String(index)])
      })
      return
    }

    const obj = toObject(node)
    if (!obj) {
      return
    }

    const statusValue =
      obj.status ??
      obj.level ??
      obj.result ??
      obj.state ??
      obj.health ??
      obj.severity ??
      (typeof obj.ok === 'boolean' ? (obj.ok ? 'ok' : 'error') : undefined) ??
      (typeof obj.success === 'boolean' ? (obj.success ? 'success' : 'error') : undefined) ??
      (typeof obj.passed === 'boolean' ? (obj.passed ? 'passed' : 'failed') : undefined)

    const normalizedStatus = toDoctorStatus(statusValue)
    if (normalizedStatus) {
      const title =
        firstString(obj, ['title', 'name', 'check', 'item', 'service', 'component', 'id']) ??
        normalizeLabel(path[path.length - 1] ?? `检查项 ${items.length + 1}`)
      const detail = firstString(obj, [
        'message',
        'detail',
        'reason',
        'error',
        'stderr',
        'description',
        'desc',
      ])

      items.push({
        key: `${path.join('.')}-${items.length}`,
        title: normalizeLabel(title),
        status: normalizedStatus,
        detail,
      })
    } else {
      for (const [key, value] of Object.entries(obj)) {
        if (typeof value === 'object' || value === null) {
          continue
        }

        const primitiveStatus = toDoctorStatus(value)
        if (!primitiveStatus) {
          continue
        }

        items.push({
          key: `${[...path, key].join('.')}-${items.length}`,
          title: normalizeLabel(key),
          status: primitiveStatus,
          detail: typeof value === 'string' ? value : undefined,
        })
      }
    }

    for (const [key, value] of Object.entries(obj)) {
      if (typeof value === 'object' && value !== null) {
        visit(value, [...path, key])
      }
    }
  }

  visit(payload, [])

  const deduped = new Map<string, DoctorCheckItem>()
  for (const item of items) {
    const uniqueKey = `${item.title}|${item.status}|${item.detail ?? ''}`
    if (!deduped.has(uniqueKey)) {
      deduped.set(uniqueKey, item)
    }
  }

  return Array.from(deduped.values())
}

function doctorStatusIcon(status: DoctorCheckStatus) {
  if (status === 'success') {
    return <CheckCircleFilled style={{ color: '#16a34a' }} />
  }
  if (status === 'warning') {
    return <ExclamationCircleFilled style={{ color: '#d97706' }} />
  }
  return <CloseCircleFilled style={{ color: '#dc2626' }} />
}

function terminalBlockStyle(maxHeight = 320): CSSProperties {
  return {
    margin: 0,
    padding: 12,
    maxHeight,
    overflow: 'auto',
    whiteSpace: 'pre-wrap',
    wordBreak: 'break-word',
    background: '#0b1020',
    color: '#d1fae5',
    borderRadius: 8,
    fontSize: 12,
    lineHeight: 1.5,
    fontFamily: 'Menlo, Monaco, Consolas, monospace',
  }
}

interface UpdateStatusInfo {
  hasUpdate: boolean
  availableVersion: string
  detail?: string
}

function readBool(value: unknown): boolean | null {
  if (typeof value === 'boolean') {
    return value
  }
  if (typeof value === 'number') {
    return value > 0
  }
  if (typeof value !== 'string') {
    return null
  }
  const normalized = toLowerText(value)
  if (!normalized) {
    return null
  }
  if (['true', 'yes', 'available', 'update', 'updates', 'new', '1'].includes(normalized)) {
    return true
  }
  if (
    ['false', 'no', 'none', 'latest', 'up-to-date', 'uptodate', 'none available', '0'].includes(
      normalized,
    )
  ) {
    return false
  }
  return null
}

function extractGatewayStatus(parsedOutput: unknown, rawOutput: string): string {
  const parsedObj = toObject(parsedOutput)
  if (parsedObj) {
    const status =
      firstString(parsedObj, ['gatewayStatus', 'status', 'state', 'health']) ??
      firstString(toObject(parsedObj.gateway) ?? {}, ['status', 'state', 'health'])
    if (status) {
      return status
    }
  }

  const lines = rawOutput
    .split('\n')
    .map((line) => line.trim())
    .filter(Boolean)
  for (const line of lines) {
    const matched = line.match(/(?:status|state|gateway)\s*[:=]\s*(.+)$/i)
    if (matched?.[1]) {
      return matched[1].trim()
    }
  }
  return lines[0] || ''
}

function parseUpdateStatus(parsedOutput: unknown, fallbackVersion?: string): UpdateStatusInfo {
  const fallback = (fallbackVersion || '').trim()
  const fallbackLower = fallback.toLowerCase()
  const fallbackHasUpdate = Boolean(fallback && !['none', 'false', 'no', '0'].includes(fallbackLower))

  let availableVersion = fallback
  let hasUpdate = fallbackHasUpdate
  let detail = ''

  const parsedObj = toObject(parsedOutput)
  if (!parsedObj) {
    return { hasUpdate, availableVersion, detail }
  }

  availableVersion =
    firstString(parsedObj, [
      'availableVersion',
      'latestVersion',
      'newVersion',
      'targetVersion',
      'version',
      'latest',
    ]) ?? availableVersion

  const updateFlagCandidates = [
    parsedObj.hasUpdate,
    parsedObj.updateAvailable,
    parsedObj.available,
    parsedObj.needsUpdate,
    parsedObj.upgradeAvailable,
  ]

  for (const candidate of updateFlagCandidates) {
    const boolValue = readBool(candidate)
    if (boolValue !== null) {
      hasUpdate = boolValue
      break
    }
  }

  if (!hasUpdate && availableVersion && availableVersion !== fallback) {
    hasUpdate = true
  }

  detail =
    firstString(parsedObj, ['message', 'detail', 'summary', 'channel']) ??
    (hasUpdate && availableVersion ? `可升级到 ${availableVersion}` : '已是最新版本')

  return { hasUpdate, availableVersion, detail }
}

function parseChannelItem(rawName: string, value: unknown): ChannelInfo | null {
  const name = (rawName || '').trim()
  if (!name) {
    return null
  }

  if (typeof value === 'string') {
    return { name, status: value.trim() || 'unknown' }
  }

  if (typeof value === 'boolean') {
    return { name, status: value ? 'connected' : 'disconnected' }
  }

  if (typeof value === 'number') {
    return { name, status: value > 0 ? 'connected' : 'disconnected' }
  }

  const obj = toObject(value)
  if (!obj) {
    return null
  }

  return {
    name: firstString(obj, ['name', 'channel']) || name,
    status: firstString(obj, ['status', 'state', 'health']) || 'unknown',
    error: firstString(obj, ['error', 'message']),
    messages: typeof obj.messages === 'number' ? obj.messages : undefined,
  }
}

function parseChannelsOutput(rawOutput: string): ChannelInfo[] {
  const trimmed = rawOutput.trim()
  if (!trimmed) {
    return []
  }

  const parsed = parseJsonOutput(trimmed)
  if (Array.isArray(parsed)) {
    return parsed
      .map((entry, index) => parseChannelItem(`channel-${index + 1}`, entry))
      .filter((item): item is ChannelInfo => item !== null)
  }

  const parsedObj = toObject(parsed)
  if (parsedObj) {
    const candidateArrays = [parsedObj.channels, parsedObj.items, parsedObj.data]
    for (const candidate of candidateArrays) {
      if (!Array.isArray(candidate)) {
        continue
      }
      const arrayItems = candidate
        .map((entry, index) => parseChannelItem(`channel-${index + 1}`, entry))
        .filter((item): item is ChannelInfo => item !== null)
      if (arrayItems.length > 0) {
        return arrayItems
      }
    }

    const mapItems = Object.entries(parsedObj)
      .map(([key, value]) => parseChannelItem(key, value))
      .filter((item): item is ChannelInfo => item !== null)
    if (mapItems.length > 0) {
      return mapItems
    }
  }

  const channels: ChannelInfo[] = []
  for (const line of trimmed.split('\n')) {
    const pureLine = line.trim()
    if (!pureLine) {
      continue
    }

    const keyValueMatch = pureLine.match(/^([A-Za-z0-9_.-]+)\s*[:=|-]\s*(.+)$/)
    if (keyValueMatch?.[1] && keyValueMatch[2]) {
      channels.push({
        name: keyValueMatch[1],
        status: keyValueMatch[2].trim(),
      })
      continue
    }

    const simpleMatch = pureLine.match(
      /^([A-Za-z0-9_.-]+)\s+(connected|disconnected|offline|online|running|error|failed)\b/i,
    )
    if (simpleMatch?.[1] && simpleMatch[2]) {
      channels.push({
        name: simpleMatch[1],
        status: simpleMatch[2],
      })
    }
  }

  const deduped = new Map<string, ChannelInfo>()
  for (const channel of channels) {
    deduped.set(channel.name, channel)
  }
  return Array.from(deduped.values())
}

function detectDoctorRepairSupport(parsedOutput: unknown): boolean | null {
  const obj = toObject(parsedOutput)
  if (!obj) {
    return null
  }

  const direct = [
    obj.repairAvailable,
    obj.autoRepair,
    obj.supportsRepair,
    obj.canRepair,
    obj.hasRepair,
  ]
  for (const candidate of direct) {
    const flag = readBool(candidate)
    if (flag !== null) {
      return flag
    }
  }

  const meta = toObject(obj.meta)
  if (meta) {
    const metaFlag = readBool(meta.repairAvailable ?? meta.supportsRepair ?? meta.canRepair)
    if (metaFlag !== null) {
      return metaFlag
    }
  }

  return null
}

function GatewayControlCard({
  device,
  onRunCommand,
  onRefreshDevice,
}: GatewayControlCardProps) {
  const [loadingAction, setLoadingAction] = useState<GatewayAction | null>(null)
  const [lastResult, setLastResult] = useState<CommandRecord | null>(null)
  const [statusLoading, setStatusLoading] = useState(false)
  const [statusText, setStatusText] = useState(device.status?.openclaw.gatewayStatus || '')
  const [statusUpdatedAt, setStatusUpdatedAt] = useState<number | null>(null)
  const statusInFlightRef = useRef(false)

  useEffect(() => {
    const nextStatus = (device.status?.openclaw.gatewayStatus || '').trim()
    if (nextStatus) {
      setStatusText(nextStatus)
    }
  }, [device.status?.openclaw.gatewayStatus])

  const refreshGatewayStatus = useCallback(
    async (showError: boolean, force = false) => {
      if (!device.online || (!force && loadingAction !== null) || statusInFlightRef.current) {
        return
      }

      statusInFlightRef.current = true
      setStatusLoading(true)
      try {
        const record = await onRunCommand(['gateway', 'status'], 20)
        const output = (record.stdout || record.stderr || '').trim()
        const parsedOutput = parseJsonOutput(output)
        const nextStatus = extractGatewayStatus(parsedOutput, output)
        if (nextStatus) {
          setStatusText(nextStatus)
        }
        setStatusUpdatedAt(record.updatedAt)
        if (isCommandFailed(record) && showError) {
          message.error(record.stderr || '获取网关状态失败')
        }
      } catch (err) {
        if (showError) {
          message.error(getErrorMessage(err, '获取网关状态失败'))
        }
      } finally {
        statusInFlightRef.current = false
        setStatusLoading(false)
      }
    },
    [device.online, loadingAction, onRunCommand],
  )

  useEffect(() => {
    if (!device.online) {
      return
    }

    let cancelled = false
    let timer: number | null = null
    const poll = async () => {
      if (cancelled) {
        return
      }
      await refreshGatewayStatus(false, true)
      if (cancelled) {
        return
      }
      timer = window.setTimeout(() => {
        void poll()
      }, GATEWAY_STATUS_POLL_INTERVAL_MS)
    }

    void poll()
    return () => {
      cancelled = true
      if (timer !== null) {
        window.clearTimeout(timer)
      }
    }
  }, [device.id, device.online, refreshGatewayStatus])

  const effectiveStatus = (statusText || device.status?.openclaw.gatewayStatus || '').trim()
  const statusMeta = gatewayStatusMeta(effectiveStatus)
  const isOffline = !device.online
  const isBusy = loadingAction !== null

  const runAction = async (action: GatewayAction) => {
    setLoadingAction(action)
    try {
      const record = await onRunCommand(['gateway', action], 30)
      setLastResult(record)

      if (isCommandFailed(record)) {
        message.error(record.stderr || `${action} 执行失败`)
      } else {
        message.success(`网关${action === 'start' ? '启动' : action === 'stop' ? '停止' : '重启'}完成`)
      }

      await onRefreshDevice()
      await refreshGatewayStatus(false, true)
    } catch (err) {
      message.error(getErrorMessage(err, '网关操作失败'))
    } finally {
      setLoadingAction(null)
    }
  }

  return (
    <Card title="网关控制">
      <Space direction="vertical" size={16} style={{ width: '100%' }}>
        <Space>
          <Typography.Text>当前状态：</Typography.Text>
          <Tag color={statusMeta.color}>{statusMeta.text || '未知'}</Tag>
          <Button
            size="small"
            icon={<ReloadOutlined />}
            loading={statusLoading}
            disabled={isOffline || isBusy}
            onClick={() => void refreshGatewayStatus(true)}
          >
            刷新状态
          </Button>
        </Space>

        {statusUpdatedAt ? (
          <Typography.Text type="secondary">状态更新时间：{formatDateTime(statusUpdatedAt)}</Typography.Text>
        ) : null}

        <Space wrap>
          <Button
            type="primary"
            icon={<PlayCircleOutlined />}
            loading={loadingAction === 'start'}
            disabled={isOffline || isBusy}
            onClick={() => void runAction('start')}
          >
            启动
          </Button>
          <Button
            icon={<PoweroffOutlined />}
            danger
            loading={loadingAction === 'stop'}
            disabled={isOffline || isBusy}
            onClick={() => void runAction('stop')}
          >
            停止
          </Button>
          <Button
            icon={<ReloadOutlined />}
            loading={loadingAction === 'restart'}
            disabled={isOffline || isBusy}
            onClick={() => void runAction('restart')}
          >
            重启
          </Button>
        </Space>

        {isOffline ? <Alert type="warning" showIcon message="设备离线，无法执行网关操作" /> : null}

        {lastResult ? (
          <Space direction="vertical" size={8} style={{ width: '100%' }}>
            <Space>
              <Typography.Text type="secondary">最近执行：</Typography.Text>
              {commandStatusTag(lastResult.status)}
              <Typography.Text type="secondary">
                {formatDateTime(lastResult.updatedAt)}
              </Typography.Text>
            </Space>
            {lastResult.stderr ? (
              <Alert type="error" showIcon message={lastResult.stderr} />
            ) : null}
          </Space>
        ) : null}
      </Space>
    </Card>
  )
}

function OpenClawUpdateCard({
  device,
  onRunCommand,
  onRefreshDevice,
}: OpenClawUpdateCardProps) {
  const [checkingStatus, setCheckingStatus] = useState(false)
  const [updating, setUpdating] = useState(false)
  const [statusResult, setStatusResult] = useState<CommandRecord | null>(null)
  const [updateResult, setUpdateResult] = useState<CommandRecord | null>(null)
  const [statusInfo, setStatusInfo] = useState<UpdateStatusInfo>(() =>
    parseUpdateStatus(null, device.status?.openclaw.updateAvailable),
  )

  const currentVersion = device.status?.openclaw.version || device.openclawVersion || '--'
  const updateAvailable = statusInfo.availableVersion
  const hasUpdate = statusInfo.hasUpdate

  useEffect(() => {
    setStatusResult(null)
    setUpdateResult(null)
    setStatusInfo(parseUpdateStatus(null, device.status?.openclaw.updateAvailable))
  }, [device.id])

  useEffect(() => {
    if (statusResult) {
      return
    }
    setStatusInfo(parseUpdateStatus(null, device.status?.openclaw.updateAvailable))
  }, [device.status?.openclaw.updateAvailable, statusResult])

  const checkUpdateStatus = useCallback(
    async (showSuccessMessage: boolean) => {
      if (!device.online || updating) {
        return
      }

      setCheckingStatus(true)
      try {
        const record = await onRunCommand(['update', 'status', '--json'], 45)
        setStatusResult(record)

        const output = (record.stdout || record.stderr || '').trim()
        const parsedOutput = parseJsonOutput(output)
        setStatusInfo(parseUpdateStatus(parsedOutput, device.status?.openclaw.updateAvailable))

        if (isCommandFailed(record)) {
          throw new Error(record.stderr || '检查更新失败')
        }
        if (showSuccessMessage) {
          message.success('更新状态已刷新')
        }
      } catch (err) {
        message.error(getErrorMessage(err, '检查更新失败'))
      } finally {
        setCheckingStatus(false)
      }
    },
    [device.online, device.status?.openclaw.updateAvailable, onRunCommand, updating],
  )

  useEffect(() => {
    if (!device.online) {
      return
    }
    void checkUpdateStatus(false)
  }, [checkUpdateStatus, device.id, device.online])

  const handleUpdate = async () => {
    setUpdating(true)
    try {
      const record = await onRunCommand(['update', '--json'], 120)
      setUpdateResult(record)

      if (isCommandFailed(record)) {
        throw new Error(record.stderr || '升级失败')
      }
      message.success('升级命令执行完成')

      await onRefreshDevice()
      await checkUpdateStatus(false)
    } catch (err) {
      message.error(getErrorMessage(err, '升级执行失败'))
    } finally {
      setUpdating(false)
    }
  }

  const output = updateResult?.stdout || updateResult?.stderr || ''
  const parsedOutput = parseJsonOutput(updateResult?.stdout)
  const updateText =
    parsedOutput !== null ? JSON.stringify(parsedOutput, null, 2) : output || '（无输出）'

  return (
    <Card
      title="OpenClaw 升级"
      extra={
        <Space>
          <Button
            icon={<ReloadOutlined />}
            loading={checkingStatus}
            disabled={!device.online || updating}
            onClick={() => void checkUpdateStatus(true)}
          >
            检查更新
          </Button>
          <Button
            type="primary"
            loading={updating}
            disabled={!device.online || checkingStatus || !hasUpdate}
            onClick={() => void handleUpdate()}
          >
            Update Now
          </Button>
        </Space>
      }
    >
      <Space direction="vertical" size={12} style={{ width: '100%' }}>
        <Space>
          <Typography.Text>当前版本：</Typography.Text>
          <Typography.Text strong>{currentVersion}</Typography.Text>
        </Space>

        <Space>
          <Typography.Text>可用更新：</Typography.Text>
          {hasUpdate ? <Tag color="processing">{updateAvailable || '有可用更新'}</Tag> : <Tag color="success">无</Tag>}
        </Space>

        <Typography.Text type="secondary">{statusInfo.detail || (hasUpdate ? '有可用更新' : '已是最新')}</Typography.Text>

        {!device.online ? <Alert type="warning" showIcon message="设备离线，无法执行升级" /> : null}

        {updating ? (
          <Space direction="vertical" style={{ width: '100%' }}>
            <Typography.Text type="secondary">升级中，请稍候...</Typography.Text>
            <Progress percent={75} status="active" showInfo={false} />
          </Space>
        ) : null}

        {statusResult ? (
          <Space>
            <Typography.Text type="secondary">状态检查：</Typography.Text>
            {commandStatusTag(statusResult.status)}
            <Typography.Text type="secondary">{formatDateTime(statusResult.updatedAt)}</Typography.Text>
          </Space>
        ) : null}

        {updateResult ? (
          <Space direction="vertical" size={8} style={{ width: '100%' }}>
            <Space>
              <Typography.Text type="secondary">升级结果：</Typography.Text>
              {commandStatusTag(updateResult.status)}
              <Typography.Text type="secondary">
                {formatDateTime(updateResult.updatedAt)}
              </Typography.Text>
            </Space>
            <pre style={terminalBlockStyle(220)}>{updateText}</pre>
          </Space>
        ) : null}
      </Space>
    </Card>
  )
}

function OpenClawInstallCard({ token, device, onRefreshDevice }: OpenClawInstallCardProps) {
  const [starting, setStarting] = useState(false)
  const [job, setJob] = useState<ConfigureIMJob | null>(null)
  const [lastError, setLastError] = useState('')

  useEffect(() => {
    setJob(null)
    setLastError('')
  }, [device.id])

  useEffect(() => {
    if (!token || !job) {
      return
    }
    if (job.status !== 'queued' && job.status !== 'running') {
      return
    }

    const timer = window.setTimeout(() => {
      fetchInstallOpenClawJob(token, device.id, job.id)
        .then((nextJob) => {
          setJob(nextJob)
          if (nextJob.status === 'success') {
            message.success('OpenClaw 安装完成')
            void onRefreshDevice()
          } else if (nextJob.status === 'failed') {
            message.error(nextJob.error || 'OpenClaw 安装失败')
            void onRefreshDevice()
          }
        })
        .catch((err) => {
          setLastError(getErrorMessage(err, '获取安装进度失败'))
        })
    }, INSTALL_JOB_POLL_INTERVAL_MS)

    return () => window.clearTimeout(timer)
  }, [device.id, job, onRefreshDevice, token])

  const startInstall = async () => {
    if (!token) {
      message.error('登录状态已失效，请重新登录')
      return
    }
    setStarting(true)
    setLastError('')
    try {
      const created = await installOpenClaw(token, device.id)
      setJob(created)
    } catch (err) {
      setLastError(getErrorMessage(err, '启动安装失败'))
      message.error(getErrorMessage(err, '启动安装失败'))
    } finally {
      setStarting(false)
    }
  }

  const detectedVersion =
    device.status?.openclaw.version || device.openclawVersion || job?.steps.find((step) => step.key === 'verify-version')?.record?.stdout || ''

  return (
    <Card
      title="OpenClaw 安装引导"
      extra={
        <Button
          type="primary"
          disabled={!device.online || starting || job?.status === 'queued' || job?.status === 'running'}
          loading={starting}
          onClick={() => void startInstall()}
        >
          Install OpenClaw
        </Button>
      }
    >
      <Space direction="vertical" size={12} style={{ width: '100%' }}>
        <Alert
          type="warning"
          showIcon
          message="当前设备未检测到 OpenClaw，建议先完成安装再进行诊断和 IM 配置。"
        />

        {!device.online ? <Alert type="error" showIcon message="设备离线，无法远程安装" /> : null}
        {lastError ? <Alert type="error" showIcon message={lastError} /> : null}

        {job ? (
          <Space direction="vertical" size={8} style={{ width: '100%' }}>
            <Space>
              <Typography.Text type="secondary">任务状态：</Typography.Text>
              {commandStatusTag(job.status)}
              <Typography.Text type="secondary">{formatDateTime(job.updatedAt)}</Typography.Text>
            </Space>
            <List
              size="small"
              bordered
              dataSource={job.steps}
              renderItem={(step) => (
                <List.Item>
                  <Space direction="vertical" size={2} style={{ width: '100%' }}>
                    <Space style={{ width: '100%', justifyContent: 'space-between' }}>
                      <Typography.Text>{step.title}</Typography.Text>
                      {installStepStatusTag(step.status)}
                    </Space>
                    <Typography.Text type="secondary">{step.displayCommand}</Typography.Text>
                    {step.error ? <Typography.Text type="danger">{step.error}</Typography.Text> : null}
                  </Space>
                </List.Item>
              )}
            />
          </Space>
        ) : null}

        {job?.status === 'success' ? (
          <Alert type="success" showIcon message={`安装成功，版本：${detectedVersion || '已安装'}`} />
        ) : null}
      </Space>
    </Card>
  )
}

function DoctorDiagnosticsCard({ device, onRunCommand }: DoctorDiagnosticsCardProps) {
  const [running, setRunning] = useState(false)
  const [repairing, setRepairing] = useState(false)
  const [result, setResult] = useState<CommandRecord | null>(null)
  const [repairResult, setRepairResult] = useState<CommandRecord | null>(null)
  const [checks, setChecks] = useState<DoctorCheckItem[]>([])
  const [rawText, setRawText] = useState('')
  const [repairSupported, setRepairSupported] = useState(true)

  useEffect(() => {
    setResult(null)
    setRepairResult(null)
    setChecks([])
    setRawText('')
    setRepairSupported(true)
  }, [device.id])

  const applyDoctorOutput = (record: CommandRecord) => {
    const rawOutput = (record.stdout || record.stderr || '').trim()
    const parsedOutput = parseJsonOutput(rawOutput)

    if (parsedOutput !== null) {
      const nextChecks = extractDoctorChecks(parsedOutput)
      setChecks(nextChecks)
      const supportFlag = detectDoctorRepairSupport(parsedOutput)
      if (supportFlag !== null) {
        setRepairSupported(supportFlag)
      }

      if (nextChecks.length === 0) {
        setRawText(JSON.stringify(parsedOutput, null, 2))
      } else {
        setRawText('')
      }
      return
    }

    setChecks([])
    setRawText(rawOutput || '（无输出）')
  }

  const handleRunDoctor = async () => {
    setRunning(true)
    try {
      const record = await onRunCommand(['doctor', '--json'], 60)
      setResult(record)
      applyDoctorOutput(record)

      if (isCommandFailed(record)) {
        message.error(record.stderr || '诊断执行失败')
      } else {
        message.success('诊断已完成')
      }
    } catch (err) {
      message.error(getErrorMessage(err, '诊断执行失败'))
    } finally {
      setRunning(false)
    }
  }

  const handleAutoRepair = async () => {
    setRepairing(true)
    try {
      const record = await onRunCommand(['doctor', '--repair', '--json'], 120)
      setRepairResult(record)
      applyDoctorOutput(record)

      if (isCommandFailed(record)) {
        message.error(record.stderr || '自动修复失败')
      } else {
        message.success('自动修复执行完成')
      }
    } catch (err) {
      message.error(getErrorMessage(err, '自动修复失败'))
    } finally {
      setRepairing(false)
    }
  }

  return (
    <Card
      title="诊断"
      extra={
        <Space>
          <Button type="primary" loading={running} disabled={!device.online || repairing} onClick={() => void handleRunDoctor()}>
            Run Diagnostics
          </Button>
          {repairSupported ? (
            <Button loading={repairing} disabled={!device.online || running} onClick={() => void handleAutoRepair()}>
              Auto Repair
            </Button>
          ) : null}
        </Space>
      }
    >
      <Space direction="vertical" size={12} style={{ width: '100%' }}>
        {!device.online ? <Alert type="warning" showIcon message="设备离线，无法运行诊断" /> : null}

        {result ? (
          <Space direction="vertical" size={8} style={{ width: '100%' }}>
            <Space>
              <Typography.Text type="secondary">执行状态：</Typography.Text>
              {commandStatusTag(result.status)}
              <Typography.Text type="secondary">{formatDateTime(result.updatedAt)}</Typography.Text>
            </Space>

            {checks.length > 0 ? (
              <List
                size="small"
                bordered
                dataSource={checks}
                renderItem={(item) => (
                  <List.Item>
                    <Space direction="vertical" size={2} style={{ width: '100%' }}>
                      <Space>
                        {doctorStatusIcon(item.status)}
                        <Typography.Text>{item.title}</Typography.Text>
                      </Space>
                      {item.detail ? (
                        <Typography.Text type="secondary">{item.detail}</Typography.Text>
                      ) : null}
                    </Space>
                  </List.Item>
                )}
              />
            ) : (
              <pre style={terminalBlockStyle(220)}>{rawText || '（无输出）'}</pre>
            )}
          </Space>
        ) : (
          <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="尚未运行诊断" />
        )}

        {repairResult ? (
          <Space>
            <Typography.Text type="secondary">最近修复：</Typography.Text>
            {commandStatusTag(repairResult.status)}
            <Typography.Text type="secondary">{formatDateTime(repairResult.updatedAt)}</Typography.Text>
          </Space>
        ) : null}
      </Space>
    </Card>
  )
}

function SystemResourcesCard({ device }: SystemResourcesCardProps) {
  const cpuPercent = Math.max(0, Math.min(100, device.status?.cpuUsage ?? 0))
  const memPercent = toProgress(device.status?.memUsed ?? 0, device.status?.memTotal ?? 0)
  const diskPercent = toProgress(device.status?.diskUsed ?? 0, device.status?.diskTotal ?? 0)

  return (
    <Card title="系统资源">
      <Space direction="vertical" size={16} style={{ width: '100%' }}>
        <Row gutter={[16, 16]}>
          <Col xs={24} sm={8}>
            <Space direction="vertical" align="center" style={{ width: '100%' }}>
              <Typography.Text>CPU</Typography.Text>
              <Progress type="circle" percent={Math.round(cpuPercent)} size={96} />
            </Space>
          </Col>
          <Col xs={24} sm={8}>
            <Space direction="vertical" align="center" style={{ width: '100%' }}>
              <Typography.Text>内存</Typography.Text>
              <Progress
                type="circle"
                percent={Math.round(memPercent)}
                size={96}
                strokeColor="#0ea5a4"
              />
            </Space>
          </Col>
          <Col xs={24} sm={8}>
            <Space direction="vertical" align="center" style={{ width: '100%' }}>
              <Typography.Text>磁盘</Typography.Text>
              <Progress
                type="circle"
                percent={Math.round(diskPercent)}
                size={96}
                strokeColor="#f59e0b"
              />
            </Space>
          </Col>
        </Row>

        <Space direction="vertical" size={4}>
          <Typography.Text>
            内存：{formatBytesToGb(device.status?.memUsed)} / {formatBytesToGb(device.status?.memTotal)}
          </Typography.Text>
          <Typography.Text>
            磁盘：{formatBytesToGb(device.status?.diskUsed)} / {formatBytesToGb(device.status?.diskTotal)}
          </Typography.Text>
          <Typography.Text>运行时长：{formatUptimeCn(device.status?.uptime)}</Typography.Text>
        </Space>
      </Space>
    </Card>
  )
}

function ChannelsStatusCard({ device, onRunCommand, onConfigure }: ChannelsStatusCardProps) {
  const [loading, setLoading] = useState(false)
  const [result, setResult] = useState<CommandRecord | null>(null)
  const [rawOutput, setRawOutput] = useState('')
  const [channels, setChannels] = useState<ChannelInfo[]>(device.status?.openclaw.channels ?? [])

  useEffect(() => {
    setResult(null)
    setRawOutput('')
    setChannels(device.status?.openclaw.channels ?? [])
  }, [device.id])

  useEffect(() => {
    if (result) {
      return
    }
    setChannels(device.status?.openclaw.channels ?? [])
  }, [device.status?.openclaw.channels, result])

  const refreshChannelsStatus = useCallback(
    async (showSuccessMessage: boolean) => {
      if (!device.online) {
        return
      }
      setLoading(true)
      try {
        const record = await onRunCommand(['channels', 'status'], 30)
        setResult(record)

        const output = (record.stdout || record.stderr || '').trim()
        const parsedChannels = parseChannelsOutput(output)
        if (parsedChannels.length > 0) {
          setChannels(parsedChannels)
          setRawOutput('')
        } else {
          setChannels(device.status?.openclaw.channels ?? [])
          setRawOutput(output)
        }

        if (isCommandFailed(record)) {
          message.error(record.stderr || '读取 IM 通道状态失败')
        } else if (showSuccessMessage) {
          message.success('IM 通道状态已刷新')
        }
      } catch (err) {
        message.error(getErrorMessage(err, '读取 IM 通道状态失败'))
      } finally {
        setLoading(false)
      }
    },
    [device.online, device.status?.openclaw.channels, onRunCommand],
  )

  useEffect(() => {
    if (!device.online) {
      return
    }
    void refreshChannelsStatus(false)
  }, [device.id, device.online, refreshChannelsStatus])

  return (
    <Card
      title="IM 通道状态"
      extra={
        <Space>
          <Button
            icon={<ReloadOutlined />}
            loading={loading}
            disabled={!device.online}
            onClick={() => void refreshChannelsStatus(true)}
          >
            刷新
          </Button>
          <Button type="primary" icon={<SettingOutlined />} onClick={onConfigure}>
            配置 IM
          </Button>
        </Space>
      }
    >
      <Space direction="vertical" size={10} style={{ width: '100%' }}>
        {!device.online ? <Alert type="warning" showIcon message="设备离线，无法读取 IM 通道状态" /> : null}

        {result ? (
          <Space>
            <Typography.Text type="secondary">最近检查：</Typography.Text>
            {commandStatusTag(result.status)}
            <Typography.Text type="secondary">{formatDateTime(result.updatedAt)}</Typography.Text>
          </Space>
        ) : null}

        {channels.length === 0 ? (
          rawOutput ? (
            <pre style={terminalBlockStyle(200)}>{rawOutput}</pre>
          ) : (
            <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无通道配置" />
          )
        ) : (
          <List
            size="small"
            bordered
            dataSource={channels}
            renderItem={(channel: ChannelInfo) => {
              const statusMeta = channelStatusMeta(channel.status)
              return (
                <List.Item>
                  <Space direction="vertical" size={2} style={{ width: '100%' }}>
                    <Space style={{ width: '100%', justifyContent: 'space-between' }}>
                      <Typography.Text strong>{channel.name || '未命名通道'}</Typography.Text>
                      <Tag color={statusMeta.color}>{statusMeta.text}</Tag>
                    </Space>
                    {channel.error ? <Typography.Text type="danger">{channel.error}</Typography.Text> : null}
                    {typeof channel.messages === 'number' ? (
                      <Typography.Text type="secondary">消息数：{channel.messages}</Typography.Text>
                    ) : null}
                  </Space>
                </List.Item>
              )
            }}
          />
        )}
      </Space>
    </Card>
  )
}

function LogViewerCard({ device, onRunCommand }: LogViewerCardProps) {
  const [loading, setLoading] = useState(false)
  const [tailMode, setTailMode] = useState(false)
  const [logs, setLogs] = useState('')
  const [result, setResult] = useState<CommandRecord | null>(null)
  const logRef = useRef<HTMLPreElement | null>(null)
  const readInFlightRef = useRef(false)

  useEffect(() => {
    setTailMode(false)
    setLogs('')
    setResult(null)
    setLoading(false)
  }, [device.id])

  const refreshLogs = useCallback(
    async (showErrorMessage: boolean, withLoading: boolean) => {
      if (!device.online || readInFlightRef.current) {
        return
      }

      readInFlightRef.current = true
      if (withLoading) {
        setLoading(true)
      }

      try {
        const record = await onRunCommand(['logs'], 30)
        setResult(record)

        const stdout = (record.stdout || '').trim()
        const stderr = (record.stderr || '').trim()
        const output = [stdout, stderr ? `【错误输出】\n${stderr}` : ''].filter(Boolean).join('\n\n')

        setLogs(output || '（无输出）')

        if (isCommandFailed(record) && showErrorMessage) {
          message.error(record.stderr || '日志获取失败')
        }
      } catch (err) {
        if (showErrorMessage) {
          message.error(getErrorMessage(err, '日志获取失败'))
        }
      } finally {
        readInFlightRef.current = false
        if (withLoading) {
          setLoading(false)
        }
      }
    },
    [device.online, onRunCommand],
  )

  useEffect(() => {
    if (logRef.current) {
      logRef.current.scrollTop = logRef.current.scrollHeight
    }
  }, [logs])

  useEffect(() => {
    if (!tailMode || !device.online) {
      return
    }

    let cancelled = false
    let timer: number | null = null

    const poll = async () => {
      if (cancelled) {
        return
      }
      await refreshLogs(false, false)
      if (cancelled) {
        return
      }
      timer = window.setTimeout(() => {
        void poll()
      }, LOG_TAIL_INTERVAL_MS)
    }

    void poll()
    return () => {
      cancelled = true
      if (timer !== null) {
        window.clearTimeout(timer)
      }
    }
  }, [tailMode, device.id, device.online, refreshLogs])

  return (
    <Card
      title="日志查看"
      extra={
        <Space>
          <Space size={6}>
            <Typography.Text type="secondary">Tail</Typography.Text>
            <Switch checked={tailMode} onChange={setTailMode} disabled={!device.online} />
          </Space>
          <Button
            type="primary"
            loading={loading}
            disabled={!device.online}
            onClick={() => void refreshLogs(true, true)}
          >
            查看日志
          </Button>
        </Space>
      }
    >
      <Space direction="vertical" size={10} style={{ width: '100%' }}>
        {!device.online ? <Alert type="warning" showIcon message="设备离线，无法读取日志" /> : null}

        {result ? (
          <Space>
            <Typography.Text type="secondary">最近读取：</Typography.Text>
            {commandStatusTag(result.status)}
            <Typography.Text type="secondary">{formatDateTime(result.updatedAt)}</Typography.Text>
          </Space>
        ) : null}

        <pre ref={logRef} style={terminalBlockStyle(420)}>
          {logs || '点击“查看日志”获取设备日志'}
        </pre>
      </Space>
    </Card>
  )
}

export function DeviceDetailPage() {
  const { id = '' } = useParams()
  const navigate = useNavigate()
  const { token } = useAuth()
  const { getDeviceById, commandRecords, refreshDevices } = useRealtime()

  const [loadingDevice, setLoadingDevice] = useState(false)
  const [deletingDevice, setDeletingDevice] = useState(false)
  const [fallbackDevice, setFallbackDevice] = useState<DeviceSnapshot | null>(null)

  const storeDevice = getDeviceById(id)
  const commandRecordsRef = useRef(commandRecords)

  useEffect(() => {
    commandRecordsRef.current = commandRecords
  }, [commandRecords])

  useEffect(() => {
    setFallbackDevice(null)
  }, [id])

  useEffect(() => {
    if (!token || !id) {
      return
    }

    let cancelled = false
    setLoadingDevice(true)

    fetchDeviceById(token, id)
      .then((detail) => {
        if (cancelled) {
          return
        }
        setFallbackDevice(detail)
      })
      .catch((err) => {
        if (cancelled) {
          return
        }
        message.error(getErrorMessage(err, '获取设备详情失败'))
      })
      .finally(() => {
        if (!cancelled) {
          setLoadingDevice(false)
        }
      })

    return () => {
      cancelled = true
    }
  }, [id, token])

  const device = useMemo(() => {
    if (!storeDevice) {
      return fallbackDevice
    }

    if (!fallbackDevice) {
      return storeDevice
    }

    return storeDevice.updatedAt >= fallbackDevice.updatedAt ? storeDevice : fallbackDevice
  }, [fallbackDevice, storeDevice])

  const refreshDevice = useCallback(async () => {
    if (!token || !id) {
      return
    }

    const detail = await fetchDeviceById(token, id)
    setFallbackDevice(detail)
  }, [id, token])

  const runCommand = useCallback<RunDeviceCommand>(
    async (args: string[], timeout: number) => {
      if (!token || !id) {
        throw new Error('缺少设备信息或登录状态')
      }

      const submitted = await execDeviceCommand(token, id, {
        command: 'openclaw',
        args,
        timeout,
      })

      let latest = commandRecordsRef.current[submitted.id] ?? submitted
      if (isTerminalStatus(latest.status)) {
        return latest
      }

      const timeoutSeconds = Math.max(timeout, 30) + 30
      const deadline = Date.now() + timeoutSeconds * 1000
      let lastPollError = ''

      while (Date.now() < deadline) {
        await waitFor(COMMAND_POLL_INTERVAL_MS)

        const wsRecord = commandRecordsRef.current[submitted.id]
        if (wsRecord) {
          latest = wsRecord
          if (isTerminalStatus(latest.status)) {
            return latest
          }
        }

        try {
          latest = await fetchCommandById(token, id, submitted.id)
        } catch (err) {
          // 忽略轮询瞬时错误，继续等待。
          lastPollError = getErrorMessage(err, '')
        }

        if (isTerminalStatus(latest.status)) {
          return latest
        }
      }

      if (!isTerminalStatus(latest.status)) {
        throw new Error(lastPollError || `命令执行超时：openclaw ${args.join(' ')}`)
      }

      return latest
    },
    [id, token],
  )

  const handleManualRefresh = async () => {
    try {
      await refreshDevice()
      message.success('设备状态已刷新')
    } catch (err) {
      message.error(getErrorMessage(err, '刷新设备失败'))
    }
  }

  const handleDeleteDevice = useCallback(() => {
    if (!token || !id) {
      return
    }

    Modal.confirm({
      title: '删除设备',
      content: '确定要删除此设备吗？此操作不可恢复。',
      okText: '删除设备',
      okType: 'danger',
      cancelText: '取消',
      onOk: async () => {
        setDeletingDevice(true)
        try {
          await deleteDevice(token, id)
          await refreshDevices()
          message.success('设备已删除')
          navigate('/dashboard')
        } catch (err) {
          message.error(getErrorMessage(err, '删除设备失败'))
        } finally {
          setDeletingDevice(false)
        }
      },
    })
  }, [id, navigate, refreshDevices, token])

  if (loadingDevice && !device) {
    return (
      <Card>
        <Skeleton active paragraph={{ rows: 8 }} />
      </Card>
    )
  }

  if (!device) {
    return <Empty description="未找到设备信息" />
  }

  return (
    <Space direction="vertical" size={16} style={{ width: '100%' }}>
      <Card>
        <Space style={{ width: '100%', justifyContent: 'space-between' }} wrap>
          <Space>
            <Button icon={<ArrowLeftOutlined />} onClick={() => navigate('/dashboard')}>
              返回
            </Button>
            <Typography.Title level={4} style={{ margin: 0 }}>
              {device.hostname || device.id}
            </Typography.Title>
            <DeviceOnlineTag online={device.online} />
          </Space>
          <Space>
            <Typography.Text type="secondary">设备 ID：{device.id}</Typography.Text>
            <Typography.Text type="secondary">最后上报：{formatDateTime(device.lastSeenAt)}</Typography.Text>
            <Button icon={<ReloadOutlined />} onClick={() => void handleManualRefresh()}>
              刷新
            </Button>
            <Button
              danger
              icon={<DeleteOutlined />}
              loading={deletingDevice}
              onClick={handleDeleteDevice}
            >
              删除设备
            </Button>
          </Space>
        </Space>
      </Card>

      {!device.hasOpenClaw ? (
        <OpenClawInstallCard token={token} device={device} onRefreshDevice={refreshDevice} />
      ) : null}

      <Row gutter={[16, 16]}>
        <Col xs={24} xl={12}>
          <GatewayControlCard device={device} onRunCommand={runCommand} onRefreshDevice={refreshDevice} />
        </Col>
        <Col xs={24} xl={12}>
          <SystemResourcesCard device={device} />
        </Col>
      </Row>

      <Row gutter={[16, 16]}>
        <Col xs={24} md={12} xl={8}>
          <OpenClawUpdateCard
            device={device}
            onRunCommand={runCommand}
            onRefreshDevice={refreshDevice}
          />
        </Col>
        <Col xs={24} md={12} xl={8}>
          <DoctorDiagnosticsCard device={device} onRunCommand={runCommand} />
        </Col>
        <Col xs={24} md={24} xl={8}>
          <ChannelsStatusCard
            device={device}
            onRunCommand={runCommand}
            onConfigure={() => navigate(`/devices/${id}/im-config`)}
          />
        </Col>
      </Row>

      <Row gutter={[16, 16]}>
        <Col span={24}>
          <LogViewerCard device={device} onRunCommand={runCommand} />
        </Col>
      </Row>
    </Space>
  )
}
