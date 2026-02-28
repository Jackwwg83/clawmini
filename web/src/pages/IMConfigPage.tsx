/* eslint-disable react-hooks/set-state-in-effect */
import {
  ArrowLeftOutlined,
  CheckCircleFilled,
  ClockCircleOutlined,
  CloseCircleFilled,
  LoadingOutlined,
} from '@ant-design/icons'
import {
  Alert,
  Button,
  Card,
  Col,
  Empty,
  Form,
  Input,
  Result,
  Row,
  Space,
  Spin,
  Steps,
  Timeline,
  Typography,
  message,
} from 'antd'
import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { execDeviceCommand, fetchCommandById, fetchDeviceById } from '../api/client'
import { DeviceOnlineTag } from '../components/DeviceOnlineTag'
import { useAuth } from '../contexts/AuthContext'
import { useRealtime } from '../contexts/RealtimeContext'
import type { CommandRecord, DeviceSnapshot } from '../types'
import { formatDateTime } from '../utils/format'

const COMMAND_POLL_INTERVAL_MS = 2000
const VERIFY_WAIT_MS = 10000
const TERMINAL_STATUS = new Set(['completed', 'failed'])

type IMPlatform = 'dingtalk' | 'feishu'
type ConfigureStepStatus = 'pending' | 'running' | 'success' | 'failed'

type VerifyState = 'idle' | 'running' | 'success' | 'failed'

interface CredentialsFormValue {
  id: string
  secret: string
}

interface CommandPlanItem {
  key: string
  title: string
  args: string[]
  timeout: number
  displayCommand: string
}

interface ConfigureTimelineItem extends CommandPlanItem {
  status: ConfigureStepStatus
  error?: string
}

interface PlatformMeta {
  name: string
  desc: string
  createHint: string
  link: string
  idLabel: string
  secretLabel: string
}

interface VerifyResult {
  ok: boolean
  message: string
  detail?: string
  channelName?: string
}

const PLATFORM_META: Record<IMPlatform, PlatformMeta> = {
  dingtalk: {
    name: '钉钉',
    desc: '适用于钉钉企业内部应用的消息接入。',
    createHint: '登录钉钉开放平台 → 创建企业内部应用 → 获取 ClientID 和 ClientSecret',
    link: 'https://open.dingtalk.com',
    idLabel: 'ClientID',
    secretLabel: 'ClientSecret',
  },
  feishu: {
    name: '飞书',
    desc: '适用于飞书企业自建应用的消息接入。',
    createHint: '登录飞书开放平台 → 创建企业自建应用 → 获取 App ID 和 App Secret',
    link: 'https://open.feishu.cn',
    idLabel: 'AppID',
    secretLabel: 'AppSecret',
  },
}

function waitFor(ms: number): Promise<void> {
  return new Promise((resolve) => {
    window.setTimeout(resolve, ms)
  })
}

function isTerminalStatus(status?: string): boolean {
  if (!status) {
    return false
  }
  return TERMINAL_STATUS.has(status)
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

function getErrorMessage(err: unknown, fallback: string): string {
  if (err instanceof Error && err.message) {
    return err.message
  }
  return fallback
}

function toLowerText(value?: string): string {
  return (value || '').trim().toLowerCase()
}

function isSuccessStatusText(value?: string): boolean {
  const normalized = toLowerText(value)
  return ['connected', 'ok', 'healthy', 'ready', 'running', 'active', 'success', 'online'].some((keyword) =>
    normalized.includes(keyword),
  )
}

function isFailedStatusText(value?: string): boolean {
  const normalized = toLowerText(value)
  return ['error', 'fail', 'failed', 'offline', 'disconnected', 'inactive', 'timeout', 'unhealthy'].some(
    (keyword) => normalized.includes(keyword),
  )
}

function platformKeywords(platform: IMPlatform): string[] {
  if (platform === 'dingtalk') {
    return ['dingtalk', '钉钉', 'clawdbot-dingtalk']
  }
  return ['feishu', 'lark', '飞书', '@openclaw/feishu']
}

function toObject(value: unknown): Record<string, unknown> | null {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    return null
  }
  return value as Record<string, unknown>
}

function firstString(obj: Record<string, unknown>, keys: string[]): string {
  for (const key of keys) {
    const value = obj[key]
    if (typeof value === 'string' && value.trim()) {
      return value.trim()
    }
  }
  return ''
}

function collectChannelEntries(input: unknown): Array<{ name: string; status: string; error: string }> {
  const entries: Array<{ name: string; status: string; error: string }> = []

  const visit = (node: unknown) => {
    if (Array.isArray(node)) {
      node.forEach((item) => {
        visit(item)
      })
      return
    }

    const obj = toObject(node)
    if (!obj) {
      return
    }

    const name = firstString(obj, ['name', 'id', 'plugin', 'type'])
    const status = firstString(obj, ['status', 'state', 'health'])
    const error = firstString(obj, ['error', 'message', 'detail', 'reason'])

    if (name || status || error) {
      entries.push({ name, status, error })
    }

    for (const value of Object.values(obj)) {
      if (typeof value === 'object' && value !== null) {
        visit(value)
      }
    }
  }

  visit(input)

  const dedup = new Map<string, { name: string; status: string; error: string }>()
  for (const item of entries) {
    const key = `${item.name}|${item.status}|${item.error}`
    dedup.set(key, item)
  }

  return Array.from(dedup.values())
}

function analyzeChannelStatus(platform: IMPlatform, stdout: string, stderr: string): VerifyResult {
  const output = (stdout || '').trim()
  const fallbackText = [output, stderr.trim()].filter(Boolean).join('\n')
  const keywords = platformKeywords(platform)

  if (output) {
    try {
      const parsed = JSON.parse(output) as unknown
      const channels = collectChannelEntries(parsed)

      const matchedChannels = channels.filter((channel) => {
        const text = `${channel.name} ${channel.status} ${channel.error}`.toLowerCase()
        return keywords.some((keyword) => text.includes(keyword.toLowerCase()))
      })

      if (matchedChannels.length > 0) {
        const successItem = matchedChannels.find((item) => isSuccessStatusText(item.status))
        if (successItem) {
          return {
            ok: true,
            message: `检测到通道状态：${successItem.status || '已连接'}`,
            channelName: successItem.name || PLATFORM_META[platform].name,
          }
        }

        const failedItem = matchedChannels.find((item) => isFailedStatusText(item.status) || Boolean(item.error))
        if (failedItem) {
          return {
            ok: false,
            message: `检测到通道异常：${failedItem.status || '状态异常'}`,
            detail: failedItem.error || fallbackText || '请检查应用凭证与网络连通性。',
            channelName: failedItem.name,
          }
        }

        return {
          ok: false,
          message: '已找到通道，但状态无法判定为已连接。',
          detail: fallbackText || '请稍后重试并检查网关日志。',
          channelName: matchedChannels[0]?.name,
        }
      }
    } catch {
      // 非 JSON 输出时走文本匹配。
    }
  }

  const normalizedText = fallbackText.toLowerCase()
  const mentionsPlatform = keywords.some((keyword) => normalizedText.includes(keyword.toLowerCase()))

  if (mentionsPlatform && isSuccessStatusText(normalizedText)) {
    return {
      ok: true,
      message: '检测到通道已连接。',
      channelName: PLATFORM_META[platform].name,
    }
  }

  if (mentionsPlatform && isFailedStatusText(normalizedText)) {
    return {
      ok: false,
      message: '检测到通道状态异常。',
      detail: fallbackText || '请检查通道配置。',
    }
  }

  return {
    ok: false,
    message: `未找到 ${PLATFORM_META[platform].name} 通道状态。`,
    detail: fallbackText || '请确认插件已安装并重试验证。',
  }
}

function buildCommandPlan(platform: IMPlatform, credential: CredentialsFormValue): CommandPlanItem[] {
  if (platform === 'dingtalk') {
    return [
      {
        key: 'install-plugin',
        title: '安装插件',
        args: ['plugins', 'install', 'clawdbot-dingtalk'],
        timeout: 120,
        displayCommand: 'openclaw plugins install clawdbot-dingtalk',
      },
      {
        key: 'set-client-id',
        title: '配置 ClientID',
        args: ['config', 'set', 'plugins.entries.clawdbot-dingtalk.clientId', credential.id],
        timeout: 15,
        displayCommand: 'openclaw config set plugins.entries.clawdbot-dingtalk.clientId <已填写>',
      },
      {
        key: 'set-client-secret',
        title: '配置 ClientSecret',
        args: ['config', 'set', 'plugins.entries.clawdbot-dingtalk.clientSecret', credential.secret],
        timeout: 15,
        displayCommand: 'openclaw config set plugins.entries.clawdbot-dingtalk.clientSecret ******',
      },
      {
        key: 'enable-ai-card',
        title: '启用 AI Card',
        args: ['config', 'set', 'plugins.entries.clawdbot-dingtalk.aiCard.enabled', 'true'],
        timeout: 15,
        displayCommand: 'openclaw config set plugins.entries.clawdbot-dingtalk.aiCard.enabled true',
      },
      {
        key: 'restart-gateway',
        title: '重启 Gateway',
        args: ['gateway', 'restart'],
        timeout: 30,
        displayCommand: 'openclaw gateway restart',
      },
    ]
  }

  return [
    {
      key: 'install-plugin',
      title: '安装插件',
      args: ['plugins', 'install', '@openclaw/feishu'],
      timeout: 120,
      displayCommand: 'openclaw plugins install @openclaw/feishu',
    },
    {
      key: 'set-app-id',
      title: '配置 AppID',
      args: ['config', 'set', 'plugins.entries.@openclaw/feishu.appId', credential.id],
      timeout: 15,
      displayCommand: 'openclaw config set plugins.entries.@openclaw/feishu.appId <已填写>',
    },
    {
      key: 'set-app-secret',
      title: '配置 AppSecret',
      args: ['config', 'set', 'plugins.entries.@openclaw/feishu.appSecret', credential.secret],
      timeout: 15,
      displayCommand: 'openclaw config set plugins.entries.@openclaw/feishu.appSecret ******',
    },
    {
      key: 'restart-gateway',
      title: '重启 Gateway',
      args: ['gateway', 'restart'],
      timeout: 30,
      displayCommand: 'openclaw gateway restart',
    },
  ]
}

function configureItemStatus(step: ConfigureTimelineItem): {
  color: string
  dot: ReactNode
} {
  if (step.status === 'running') {
    return {
      color: 'blue',
      dot: <LoadingOutlined spin style={{ color: '#1677ff' }} />,
    }
  }

  if (step.status === 'success') {
    return {
      color: 'green',
      dot: <CheckCircleFilled style={{ color: '#16a34a' }} />,
    }
  }

  if (step.status === 'failed') {
    return {
      color: 'red',
      dot: <CloseCircleFilled style={{ color: '#dc2626' }} />,
    }
  }

  return {
    color: 'gray',
    dot: <ClockCircleOutlined style={{ color: '#9ca3af' }} />,
  }
}

export function IMConfigPage() {
  const { id = '' } = useParams()
  const navigate = useNavigate()
  const { token } = useAuth()
  const { getDeviceById, commandRecords } = useRealtime()

  const [form] = Form.useForm<CredentialsFormValue>()
  const [loadingDevice, setLoadingDevice] = useState(false)
  const [fallbackDevice, setFallbackDevice] = useState<DeviceSnapshot | null>(null)

  const [currentStep, setCurrentStep] = useState(0)
  const [platform, setPlatform] = useState<IMPlatform | null>(null)
  const [credentials, setCredentials] = useState<CredentialsFormValue | null>(null)

  const [configureState, setConfigureState] = useState<'idle' | 'running' | 'success' | 'failed'>('idle')
  const [configureError, setConfigureError] = useState('')
  const [configureSteps, setConfigureSteps] = useState<ConfigureTimelineItem[]>([])

  const [verifyState, setVerifyState] = useState<VerifyState>('idle')
  const [verifyMessage, setVerifyMessage] = useState('')
  const [verifyChannelName, setVerifyChannelName] = useState('')
  const [verifyRecord, setVerifyRecord] = useState<CommandRecord | null>(null)

  const storeDevice = getDeviceById(id)
  const commandRecordsRef = useRef(commandRecords)
  const cancelledRef = useRef(false)

  useEffect(() => {
    commandRecordsRef.current = commandRecords
  }, [commandRecords])

  useEffect(() => {
    setFallbackDevice(null)
  }, [id])

  useEffect(() => {
    cancelledRef.current = false
    return () => {
      cancelledRef.current = true
    }
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

  const platformMeta = platform ? PLATFORM_META[platform] : null

  const runCommand = useCallback(
    async (args: string[], timeout: number): Promise<CommandRecord> => {
      if (!token || !id) {
        throw new Error('缺少登录状态或设备 ID')
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
          if (isTerminalStatus(latest.status)) {
            return latest
          }
        } catch (err) {
          lastPollError = getErrorMessage(err, '')
        }
      }

      throw new Error(lastPollError || `命令执行超时：openclaw ${args.join(' ')}`)
    },
    [id, token],
  )

  const resetExecutionState = () => {
    setConfigureState('idle')
    setConfigureError('')
    setConfigureSteps([])
    setVerifyState('idle')
    setVerifyMessage('')
    setVerifyChannelName('')
    setVerifyRecord(null)
  }

  const handleSelectPlatform = (nextPlatform: IMPlatform) => {
    setPlatform(nextPlatform)
    setCredentials(null)
    form.resetFields()
    resetExecutionState()
    setCurrentStep(1)
  }

  const runConfigureFlow = useCallback(
    async (nextCredential: CredentialsFormValue) => {
      if (!platform) {
        message.error('请先选择平台')
        return
      }
      if (cancelledRef.current) {
        return
      }

      const plan = buildCommandPlan(platform, nextCredential)
      setConfigureState('running')
      setConfigureError('')
      setVerifyState('idle')
      setVerifyMessage('')
      setVerifyChannelName('')
      setVerifyRecord(null)
      setConfigureSteps(
        plan.map((item) => ({
          ...item,
          status: 'pending',
        })),
      )

      for (let index = 0; index < plan.length; index += 1) {
        if (cancelledRef.current) {
          return
        }
        const item = plan[index]
        setConfigureSteps((prev) =>
          prev.map((step, stepIndex) => {
            if (stepIndex === index) {
              return { ...step, status: 'running', error: undefined }
            }
            return step
          }),
        )

        try {
          const record = await runCommand(item.args, item.timeout)
          if (cancelledRef.current) {
            return
          }

          if (isCommandFailed(record)) {
            const errorText = record.stderr || `${item.title} 失败`
            setConfigureSteps((prev) =>
              prev.map((step, stepIndex) => {
                if (stepIndex === index) {
                  return { ...step, status: 'failed', error: errorText }
                }
                return step
              }),
            )
            setConfigureState('failed')
            setConfigureError(errorText)
            return
          }

          setConfigureSteps((prev) =>
            prev.map((step, stepIndex) => {
              if (stepIndex === index) {
                return { ...step, status: 'success', error: undefined }
              }
              return step
            }),
          )
        } catch (err) {
          if (cancelledRef.current) {
            return
          }
          const errorText = getErrorMessage(err, `${item.title} 执行失败`)
          setConfigureSteps((prev) =>
            prev.map((step, stepIndex) => {
              if (stepIndex === index) {
                return { ...step, status: 'failed', error: errorText }
              }
              return step
            }),
          )
          setConfigureState('failed')
          setConfigureError(errorText)
          return
        }
      }

      if (cancelledRef.current) {
        return
      }
      setConfigureState('success')
      setCurrentStep(4)
      message.success('自动配置完成，开始验证连接')
    },
    [platform, runCommand],
  )

  const handleSubmitCredential = async () => {
    try {
      const values = await form.validateFields()
      cancelledRef.current = false
      setCredentials(values)
      setCurrentStep(3)
      void runConfigureFlow(values)
    } catch {
      // 表单错误由 Form 自身展示。
    }
  }

  useEffect(() => {
    if (currentStep !== 4 || configureState !== 'success' || verifyState !== 'idle' || !platform) {
      return
    }

    let cancelled = false

    const runVerify = async () => {
      setVerifyState('running')
      setVerifyMessage('等待网关重启后验证连接状态...')

      try {
        await waitFor(VERIFY_WAIT_MS)

        if (cancelled) {
          return
        }

        const record = await runCommand(['channels', 'status'], 15)
        if (cancelled) {
          return
        }

        setVerifyRecord(record)

        if (isCommandFailed(record)) {
          setVerifyState('failed')
          setVerifyMessage(record.stderr || '获取通道状态失败')
          return
        }

        const analyzed = analyzeChannelStatus(platform, record.stdout || '', record.stderr || '')
        setVerifyState(analyzed.ok ? 'success' : 'failed')
        setVerifyMessage(analyzed.detail ? `${analyzed.message}\n${analyzed.detail}` : analyzed.message)
        setVerifyChannelName(analyzed.channelName || PLATFORM_META[platform].name)
      } catch (err) {
        if (cancelled) {
          return
        }
        setVerifyState('failed')
        setVerifyMessage(getErrorMessage(err, '验证连接失败'))
      }
    }

    void runVerify()

    return () => {
      cancelled = true
    }
  }, [configureState, currentStep, platform, runCommand, verifyState])

  const stepItems = [
    { title: '选择平台' },
    { title: '创建应用' },
    { title: '填写凭证' },
    { title: '自动配置' },
    { title: '验证连接' },
  ]

  const troubleshootingTips = [
    '确认平台应用凭证填写正确且未过期。',
    '确认设备网络可访问平台开放接口。',
    '查看设备详情页日志，检查插件启动报错。',
    '若刚完成重启，可等待 10-20 秒后再次验证。',
  ]

  if (loadingDevice && !device) {
    return (
      <div className="center-block">
        <Spin tip="加载设备信息..." />
      </div>
    )
  }

  if (!device) {
    return <Empty description="未找到设备信息" />
  }

  const renderStepContent = () => {
    if (currentStep === 0) {
      return (
        <Space direction="vertical" size={16} style={{ width: '100%' }}>
          <Typography.Title level={5} style={{ margin: 0 }}>
            请选择需要配置的 IM 平台
          </Typography.Title>
          <Row gutter={[16, 16]}>
            {(Object.keys(PLATFORM_META) as IMPlatform[]).map((item) => {
              const selected = platform === item
              const meta = PLATFORM_META[item]

              return (
                <Col key={item} xs={24} md={12}>
                  <Card
                    hoverable
                    onClick={() => handleSelectPlatform(item)}
                    style={{
                      borderColor: selected ? '#1677ff' : undefined,
                      boxShadow: selected ? '0 0 0 2px rgba(22,119,255,0.2)' : undefined,
                    }}
                  >
                    <Space direction="vertical" size={12} style={{ width: '100%' }}>
                      <div
                        style={{
                          width: 56,
                          height: 56,
                          borderRadius: 12,
                          background: '#f0f5ff',
                          display: 'flex',
                          alignItems: 'center',
                          justifyContent: 'center',
                          fontWeight: 700,
                          fontSize: 16,
                          color: '#1d4ed8',
                        }}
                      >
                        {meta.name.slice(0, 2)}
                      </div>
                      <Typography.Title level={5} style={{ margin: 0 }}>
                        {meta.name}
                      </Typography.Title>
                      <Typography.Text type="secondary">{meta.desc}</Typography.Text>
                    </Space>
                  </Card>
                </Col>
              )
            })}
          </Row>
        </Space>
      )
    }

    if (currentStep === 1) {
      if (!platformMeta) {
        return <Alert type="warning" showIcon message="请先返回上一步选择平台" />
      }

      return (
        <Space direction="vertical" size={16} style={{ width: '100%' }}>
          <Alert
            type="info"
            showIcon
            message={platformMeta.createHint}
            description={
              <a href={platformMeta.link} target="_blank" rel="noreferrer">
                打开平台：{platformMeta.link}
              </a>
            }
          />

          <Space>
            <Button onClick={() => setCurrentStep(0)}>上一步</Button>
            <Button type="primary" onClick={() => setCurrentStep(2)}>
              我已创建
            </Button>
          </Space>
        </Space>
      )
    }

    if (currentStep === 2) {
      if (!platformMeta) {
        return <Alert type="warning" showIcon message="请先返回上一步选择平台" />
      }

      return (
        <Space direction="vertical" size={16} style={{ width: '100%' }}>
          <Form
            form={form}
            layout="vertical"
            initialValues={credentials || { id: '', secret: '' }}
            style={{ maxWidth: 520 }}
          >
            <Form.Item
              label={platformMeta.idLabel}
              name="id"
              rules={[
                { required: true, message: `请输入 ${platformMeta.idLabel}` },
                { min: 10, message: `${platformMeta.idLabel} 至少 10 位` },
              ]}
            >
              <Input autoComplete="off" placeholder={`请输入 ${platformMeta.idLabel}`} />
            </Form.Item>
            <Form.Item
              label={platformMeta.secretLabel}
              name="secret"
              rules={[
                { required: true, message: `请输入 ${platformMeta.secretLabel}` },
                { min: 10, message: `${platformMeta.secretLabel} 至少 10 位` },
              ]}
            >
              <Input.Password autoComplete="new-password" placeholder={`请输入 ${platformMeta.secretLabel}`} />
            </Form.Item>
          </Form>

          <Space>
            <Button onClick={() => setCurrentStep(1)}>上一步</Button>
            <Button type="primary" onClick={() => void handleSubmitCredential()}>
              下一步
            </Button>
          </Space>
        </Space>
      )
    }

    if (currentStep === 3) {
      return (
        <Space direction="vertical" size={16} style={{ width: '100%' }}>
          <Typography.Text type="secondary">按顺序执行配置命令，请勿关闭页面。</Typography.Text>

          <Timeline
            items={configureSteps.map((step, index) => {
              const { color, dot } = configureItemStatus(step)
              return {
                color,
                dot,
                children: (
                  <Space direction="vertical" size={2} style={{ width: '100%' }}>
                    <Typography.Text>{`${index + 1}. ${step.title}...`}</Typography.Text>
                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                      {step.displayCommand}
                    </Typography.Text>
                    {step.error ? <Typography.Text type="danger">{step.error}</Typography.Text> : null}
                  </Space>
                ),
              }
            })}
          />

          {configureState === 'running' ? (
            <Alert type="info" showIcon message="正在执行自动配置，请稍候..." />
          ) : null}

          {configureState === 'failed' ? (
            <Space direction="vertical" size={12} style={{ width: '100%' }}>
              <Alert type="error" showIcon message="自动配置失败" description={configureError || '请重试'} />
              <Button
                type="primary"
                onClick={() => {
                  if (!credentials) {
                    setCurrentStep(2)
                    return
                  }
                  void runConfigureFlow(credentials)
                }}
              >
                重试
              </Button>
            </Space>
          ) : null}
        </Space>
      )
    }

    return (
      <Space direction="vertical" size={16} style={{ width: '100%' }}>
        {verifyState === 'running' ? (
          <Result
            icon={<Spin />}
            title="正在验证连接"
            subTitle={verifyMessage || '正在检查通道状态，请稍候...'}
          />
        ) : null}

        {verifyState === 'success' ? (
          <Result
            status="success"
            title={`✅ ${verifyChannelName || platformMeta?.name || 'IM'}已连接`}
            subTitle={verifyMessage || '通道状态正常'}
            extra={
              <Button type="primary" onClick={() => navigate(`/devices/${id}`)}>
                完成
              </Button>
            }
          />
        ) : null}

        {verifyState === 'failed' ? (
          <Space direction="vertical" size={12} style={{ width: '100%' }}>
            <Result
              status="error"
              title="❌ 连接失败"
              subTitle={verifyMessage || '请根据排查建议检查配置'}
              extra={
                <Space>
                  <Button
                    onClick={() => {
                      setVerifyState('idle')
                      setVerifyMessage('')
                      setVerifyRecord(null)
                    }}
                  >
                    重试验证
                  </Button>
                  <Button type="primary" onClick={() => navigate(`/devices/${id}`)}>
                    完成
                  </Button>
                </Space>
              }
            />

            <Alert
              type="warning"
              showIcon
              message="排查建议"
              description={
                <Space direction="vertical" size={4}>
                  {troubleshootingTips.map((tip) => (
                    <Typography.Text key={tip}>{tip}</Typography.Text>
                  ))}
                </Space>
              }
            />

            {verifyRecord?.stderr ? (
              <Alert type="error" showIcon message="命令错误输出" description={verifyRecord.stderr} />
            ) : null}
          </Space>
        ) : null}
      </Space>
    )
  }

  return (
    <Space direction="vertical" size={16} style={{ width: '100%' }}>
      <Card>
        <Space style={{ width: '100%', justifyContent: 'space-between' }} wrap>
          <Space>
            <Button icon={<ArrowLeftOutlined />} onClick={() => navigate(`/devices/${id}`)}>
              返回设备详情
            </Button>
            <Typography.Title level={4} style={{ margin: 0 }}>
              IM 配置向导
            </Typography.Title>
            <DeviceOnlineTag online={device.online} />
          </Space>
          <Space direction="vertical" size={0} style={{ textAlign: 'right' }}>
            <Typography.Text>设备：{device.hostname || device.id}</Typography.Text>
            <Typography.Text type="secondary">最后上报：{formatDateTime(device.lastSeenAt)}</Typography.Text>
          </Space>
        </Space>
      </Card>

      {!device.online ? <Alert type="warning" showIcon message="设备当前离线，执行命令可能失败。" /> : null}

      <Card>
        <Space direction="vertical" size={20} style={{ width: '100%' }}>
          <Steps current={currentStep} items={stepItems} responsive />
          {renderStepContent()}
        </Space>
      </Card>
    </Space>
  )
}
