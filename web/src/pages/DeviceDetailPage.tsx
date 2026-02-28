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
  Space,
  Spin,
  Tag,
  Typography,
  message,
} from 'antd'
import { useCallback, useEffect, useMemo, useRef, useState, type CSSProperties } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { deleteDevice, execDeviceCommand, fetchCommandById, fetchDeviceById } from '../api/client'
import { DeviceOnlineTag } from '../components/DeviceOnlineTag'
import { useAuth } from '../contexts/AuthContext'
import { useRealtime } from '../contexts/RealtimeContext'
import type { ChannelInfo, CommandRecord, DeviceSnapshot } from '../types'
import { formatDateTime, toProgress } from '../utils/format'

const TERMINAL_STATUS = new Set(['completed', 'failed'])
const COMMAND_POLL_INTERVAL_MS = 1200

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

interface DoctorDiagnosticsCardProps {
  device: DeviceSnapshot
  onRunCommand: RunDeviceCommand
}

interface SystemResourcesCardProps {
  device: DeviceSnapshot
}

interface ChannelsStatusCardProps {
  device: DeviceSnapshot
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

function GatewayControlCard({
  device,
  onRunCommand,
  onRefreshDevice,
}: GatewayControlCardProps) {
  const [loadingAction, setLoadingAction] = useState<GatewayAction | null>(null)
  const [lastResult, setLastResult] = useState<CommandRecord | null>(null)

  const statusText = device.status?.openclaw.gatewayStatus
  const statusMeta = gatewayStatusMeta(statusText)
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
          <Tag color={statusMeta.color}>{statusMeta.text}</Tag>
        </Space>

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
  const [updating, setUpdating] = useState(false)
  const [updateResult, setUpdateResult] = useState<CommandRecord | null>(null)

  const currentVersion = device.status?.openclaw.version || device.openclawVersion || '--'
  const updateAvailable = (device.status?.openclaw.updateAvailable || '').trim()
  const updateLower = updateAvailable.toLowerCase()
  const hasUpdate = Boolean(updateAvailable && !['none', 'false', 'no', '0'].includes(updateLower))

  const handleUpdate = async () => {
    setUpdating(true)
    try {
      const record = await onRunCommand(['update', '--json'], 120)
      setUpdateResult(record)

      if (isCommandFailed(record)) {
        message.error(record.stderr || '升级失败')
      } else {
        message.success('升级命令执行完成')
      }

      await onRefreshDevice()
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
        hasUpdate ? (
          <Button type="primary" loading={updating} disabled={!device.online} onClick={() => void handleUpdate()}>
            升级到 {updateAvailable}
          </Button>
        ) : (
          <Tag color="default">已是最新</Tag>
        )
      }
    >
      <Space direction="vertical" size={12} style={{ width: '100%' }}>
        <Space>
          <Typography.Text>当前版本：</Typography.Text>
          <Typography.Text strong>{currentVersion}</Typography.Text>
        </Space>

        <Space>
          <Typography.Text>可用更新：</Typography.Text>
          {hasUpdate ? <Tag color="processing">{updateAvailable}</Tag> : <Tag color="success">无</Tag>}
        </Space>

        {!device.online ? <Alert type="warning" showIcon message="设备离线，无法执行升级" /> : null}

        {updating ? (
          <Space direction="vertical" style={{ width: '100%' }}>
            <Typography.Text type="secondary">升级中，请稍候...</Typography.Text>
            <Progress percent={70} status="active" showInfo={false} />
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

function DoctorDiagnosticsCard({ device, onRunCommand }: DoctorDiagnosticsCardProps) {
  const [running, setRunning] = useState(false)
  const [result, setResult] = useState<CommandRecord | null>(null)
  const [checks, setChecks] = useState<DoctorCheckItem[]>([])
  const [rawText, setRawText] = useState('')

  const handleRunDoctor = async () => {
    setRunning(true)
    try {
      const record = await onRunCommand(['doctor', '--json'], 60)
      setResult(record)

      const rawOutput = (record.stdout || record.stderr || '').trim()
      const parsedOutput = parseJsonOutput(rawOutput)

      if (parsedOutput !== null) {
        const nextChecks = extractDoctorChecks(parsedOutput)
        setChecks(nextChecks)

        if (nextChecks.length === 0) {
          setRawText(JSON.stringify(parsedOutput, null, 2))
        } else {
          setRawText('')
        }
      } else {
        setChecks([])
        setRawText(rawOutput || '（无输出）')
      }

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

  return (
    <Card
      title="诊断"
      extra={
        <Button type="primary" loading={running} disabled={!device.online} onClick={() => void handleRunDoctor()}>
          运行诊断
        </Button>
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

function ChannelsStatusCard({ device, onConfigure }: ChannelsStatusCardProps) {
  const channels = device.status?.openclaw.channels ?? []

  return (
    <Card
      title="IM 通道状态"
      extra={
        <Button type="primary" icon={<SettingOutlined />} onClick={onConfigure}>
          配置 IM
        </Button>
      }
    >
      {channels.length === 0 ? (
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无通道配置" />
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
    </Card>
  )
}

function LogViewerCard({ device, onRunCommand }: LogViewerCardProps) {
  const [loading, setLoading] = useState(false)
  const [logs, setLogs] = useState('')
  const [result, setResult] = useState<CommandRecord | null>(null)
  const logRef = useRef<HTMLPreElement | null>(null)

  const handleViewLogs = async () => {
    setLoading(true)
    try {
      const record = await onRunCommand(['logs'], 30)
      setResult(record)

      const stdout = (record.stdout || '').trim()
      const stderr = (record.stderr || '').trim()
      const output = [stdout, stderr ? `【错误输出】\n${stderr}` : ''].filter(Boolean).join('\n\n')

      setLogs(output || '（无输出）')

      if (isCommandFailed(record)) {
        message.error(record.stderr || '日志获取失败')
      }
    } catch (err) {
      message.error(getErrorMessage(err, '日志获取失败'))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    if (logRef.current) {
      logRef.current.scrollTop = logRef.current.scrollHeight
    }
  }, [logs])

  return (
    <Card
      title="日志查看"
      extra={
        <Button type="primary" loading={loading} disabled={!device.online} onClick={() => void handleViewLogs()}>
          查看日志
        </Button>
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
      <div className="center-block">
        <Spin tip="加载设备详情..." />
      </div>
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
          <ChannelsStatusCard device={device} onConfigure={() => navigate(`/devices/${id}/im-config`)} />
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
