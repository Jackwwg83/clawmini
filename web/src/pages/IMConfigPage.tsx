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
  Empty,
  Form,
  Input,
  Result,
  Skeleton,
  Space,
  Spin,
  Steps,
  Timeline,
  Typography,
  message,
} from 'antd'
import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import {
  type ConfigureIMStep,
  execDeviceCommand,
  fetchCommandById,
  fetchConfigureIMJob,
  fetchDeviceById,
  startConfigureIM,
} from '../api/client'
import { DeviceOnlineTag } from '../components/DeviceOnlineTag'
import { useAuth } from '../contexts/AuthContext'
import { useRealtime } from '../contexts/RealtimeContext'
import type { CommandRecord, DeviceSnapshot } from '../types'
import { formatDateTime } from '../utils/format'

const COMMAND_POLL_INTERVAL_MS = 2000
const CONFIGURE_JOB_POLL_INTERVAL_MS = 1500
const VERIFY_POLL_INTERVAL_MS = 5000
const VERIFY_TIMEOUT_MS = 60000
const TERMINAL_STATUS = new Set(['completed', 'failed'])

type IMPlatform = 'dingtalk' | 'feishu'
type VerifyState = 'idle' | 'running' | 'success' | 'failed'

interface CredentialsFormValue {
  id: string
  secret: string
}

interface PlatformMeta {
  name: string
  link: string
  idLabel: string
  secretLabel: string
  instructionItems: string[]
  callbackPlaceholder: string
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
    link: 'https://open.dingtalk.com',
    idLabel: 'ClientID',
    secretLabel: 'ClientSecret',
    instructionItems: [
      '登录钉钉开放平台并创建企业内部应用。',
      '在应用凭证页面获取 ClientID 和 ClientSecret。',
      '在事件订阅/回调设置中配置回调地址并保存。',
    ],
    callbackPlaceholder: 'https://<你的网关域名>/callbacks/dingtalk',
  },
  feishu: {
    name: '飞书',
    link: 'https://open.feishu.cn',
    idLabel: 'AppID',
    secretLabel: 'AppSecret',
    instructionItems: [
      '登录飞书开放平台并创建企业自建应用。',
      '在凭证与基础信息页面获取 App ID 和 App Secret。',
      '在事件订阅中配置回调地址并启用对应事件。',
    ],
    callbackPlaceholder: 'https://<你的网关域名>/callbacks/feishu',
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

function isAbortError(err: unknown): boolean {
  return err instanceof Error && err.name === 'AbortError'
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
  return ['error', 'fail', 'failed', 'offline', 'disconnected', 'inactive', 'timeout', 'unhealthy'].some((keyword) =>
    normalized.includes(keyword),
  )
}

function platformKeywords(platform: IMPlatform): string[] {
  if (platform === 'dingtalk') {
    return ['dingtalk', '钉钉', 'clawdbot-dingtalk']
  }
  return ['feishu', 'lark', '飞书', '@anthropic-ai/feishu', '@anthropic-ai/lark']
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

function configureItemStatus(step: ConfigureIMStep): {
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

  if (step.status === 'skipped') {
    return {
      color: 'gray',
      dot: <ClockCircleOutlined style={{ color: '#9ca3af' }} />,
    }
  }

  return {
    color: 'gray',
    dot: <ClockCircleOutlined style={{ color: '#9ca3af' }} />,
  }
}

export function IMConfigPage() {
  const { id = '', platform: platformParam = '' } = useParams()
  const navigate = useNavigate()
  const { token } = useAuth()
  const { getDeviceById, commandRecords } = useRealtime()

  const [form] = Form.useForm<CredentialsFormValue>()
  const [loadingDevice, setLoadingDevice] = useState(false)
  const [fallbackDevice, setFallbackDevice] = useState<DeviceSnapshot | null>(null)

  const [currentStep, setCurrentStep] = useState(0)
  const [credentials, setCredentials] = useState<CredentialsFormValue | null>(null)

  const [configureState, setConfigureState] = useState<'idle' | 'running' | 'success' | 'failed'>('idle')
  const [configureError, setConfigureError] = useState('')
  const [configureSteps, setConfigureSteps] = useState<ConfigureIMStep[]>([])

  const [verifyState, setVerifyState] = useState<VerifyState>('idle')
  const [verifyMessage, setVerifyMessage] = useState('')
  const [verifyChannelName, setVerifyChannelName] = useState('')
  const [verifyRecord, setVerifyRecord] = useState<CommandRecord | null>(null)

  const storeDevice = getDeviceById(id)
  const commandRecordsRef = useRef(commandRecords)
  const cancelledRef = useRef(false)
  const abortControllerRef = useRef<AbortController | null>(null)

  const platform = useMemo<IMPlatform | null>(() => {
    if (platformParam === 'dingtalk' || platformParam === 'feishu') {
      return platformParam
    }
    return null
  }, [platformParam])

  useEffect(() => {
    commandRecordsRef.current = commandRecords
  }, [commandRecords])

  useEffect(() => {
    setFallbackDevice(null)
  }, [id])

  useEffect(() => {
    cancelledRef.current = false
    abortControllerRef.current?.abort()
    const controller = new AbortController()
    abortControllerRef.current = controller
    return () => {
      cancelledRef.current = true
      controller.abort()
      if (abortControllerRef.current === controller) {
        abortControllerRef.current = null
      }
    }
  }, [id, platformParam])

  useEffect(() => {
    if (!token || !id) {
      return
    }

    let cancelled = false
    setLoadingDevice(true)

    fetchDeviceById(token, id, abortControllerRef.current?.signal)
      .then((detail) => {
        if (cancelled) {
          return
        }
        setFallbackDevice(detail)
      })
      .catch((err) => {
        if (isAbortError(err)) {
          return
        }
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
      const signal = abortControllerRef.current?.signal
      if (signal?.aborted) {
        throw new Error('请求已取消')
      }

      const submitted = await execDeviceCommand(
        token,
        id,
        {
          command: 'openclaw',
          args,
          timeout,
        },
        signal,
      )

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
          latest = await fetchCommandById(token, id, submitted.id, signal)
          if (isTerminalStatus(latest.status)) {
            return latest
          }
        } catch (err) {
          if (isAbortError(err)) {
            throw err
          }
          lastPollError = getErrorMessage(err, '')
        }
      }

      throw new Error(lastPollError || `命令执行超时：openclaw ${args.join(' ')}`)
    },
    [id, token],
  )

  const runConfigureFlow = useCallback(
    async (nextCredential: CredentialsFormValue) => {
      if (!platform || !token || !id) {
        message.error('缺少平台、登录状态或设备 ID')
        return
      }

      setConfigureState('running')
      setConfigureError('')
      setVerifyState('idle')
      setVerifyMessage('')
      setVerifyChannelName('')
      setVerifyRecord(null)

      try {
        const signal = abortControllerRef.current?.signal
        if (signal?.aborted) {
          return
        }
        const createdJob = await startConfigureIM(
          token,
          id,
          {
            platform,
            credentials: {
              id: nextCredential.id,
              secret: nextCredential.secret,
            },
          },
          signal,
        )

        if (cancelledRef.current) {
          return
        }

        setConfigureSteps(createdJob.steps)

        let latest = createdJob
        while (!cancelledRef.current) {
          if (latest.status === 'success') {
            setConfigureState('success')
            setConfigureError('')
            setCurrentStep(3)
            message.success('自动配置完成，开始验证连接')
            return
          }

          if (latest.status === 'failed') {
            const failedStep = latest.steps.find((step) => step.status === 'failed')
            const errorText = latest.error || failedStep?.error || '自动配置失败'
            setConfigureState('failed')
            setConfigureError(errorText)
            return
          }

          await waitFor(CONFIGURE_JOB_POLL_INTERVAL_MS)
          if (cancelledRef.current) {
            return
          }

          latest = await fetchConfigureIMJob(token, id, createdJob.id, signal)
          setConfigureSteps(latest.steps)
        }
      } catch (err) {
        if (isAbortError(err)) {
          return
        }
        if (cancelledRef.current) {
          return
        }
        const errorText = getErrorMessage(err, '自动配置失败')
        setConfigureState('failed')
        setConfigureError(errorText)
      }
    },
    [id, platform, token],
  )

  const handleSubmitCredential = async () => {
    try {
      const values = await form.validateFields()
      cancelledRef.current = false
      setCredentials(values)
      setCurrentStep(2)
      void runConfigureFlow(values)
    } catch {
      // 表单错误由 Form 自身展示。
    }
  }

  useEffect(() => {
    if (currentStep !== 3 || configureState !== 'success' || verifyState !== 'idle' || !platform) {
      return
    }

    let cancelled = false

    const runVerify = async () => {
      setVerifyState('running')
      setVerifyChannelName('')
      setVerifyRecord(null)
      setVerifyMessage('开始轮询通道状态...')

      let lastFailure = ''
      const startedAt = Date.now()
      let attempt = 0

      while (!cancelled && Date.now() - startedAt <= VERIFY_TIMEOUT_MS) {
        attempt += 1
        setVerifyMessage(`第 ${attempt} 次检查通道状态...`)

        try {
          const record = await runCommand(['channels', 'status'], 15)
          if (cancelled) {
            return
          }

          setVerifyRecord(record)

          if (!isCommandFailed(record)) {
            const analyzed = analyzeChannelStatus(platform, record.stdout || '', record.stderr || '')
            if (analyzed.ok) {
              setVerifyState('success')
              setVerifyMessage(analyzed.message)
              setVerifyChannelName(analyzed.channelName || PLATFORM_META[platform].name)
              return
            }

            lastFailure = analyzed.detail ? `${analyzed.message}\n${analyzed.detail}` : analyzed.message
          } else {
            lastFailure = record.stderr || '获取通道状态失败'
          }
        } catch (err) {
          lastFailure = getErrorMessage(err, '验证连接失败')
        }

        if (Date.now() - startedAt >= VERIFY_TIMEOUT_MS) {
          break
        }

        await waitFor(VERIFY_POLL_INTERVAL_MS)
      }

      if (cancelled) {
        return
      }

      setVerifyState('failed')
      setVerifyMessage(lastFailure || '在 60 秒内未检测到通道连接，请重试。')
    }

    void runVerify()

    return () => {
      cancelled = true
    }
  }, [configureState, currentStep, platform, runCommand, verifyState])

  const stepItems = [
    { title: '说明' },
    { title: '凭证' },
    { title: '配置' },
    { title: '验证' },
  ]

  const troubleshootingTips = [
    '确认平台应用凭证填写正确且未过期。',
    '确认设备网络可访问平台开放接口。',
    '查看设备详情页日志，检查插件启动报错。',
    '可点击“重试验证”，系统会再次轮询 60 秒。',
  ]

  if (loadingDevice && !device) {
    return (
      <Card>
        <Skeleton active paragraph={{ rows: 8 }} />
      </Card>
    )
  }

  if (!platformMeta) {
    return (
      <Result
        status="warning"
        title="不支持的 IM 平台"
        subTitle="请从 IM 配置入口重新选择钉钉或飞书。"
        extra={
          <Button type="primary" onClick={() => navigate('/im-config')}>
            返回 IM 配置入口
          </Button>
        }
      />
    )
  }

  if (!device) {
    return <Empty description="未找到设备信息" />
  }

  const renderStepContent = () => {
    if (currentStep === 0) {
      return (
        <Space direction="vertical" size={16} style={{ width: '100%' }}>
          <Alert
            type="info"
            showIcon
            message={`在 ${platformMeta.name} 开放平台完成应用创建后再继续`}
            description={
              <a href={platformMeta.link} target="_blank" rel="noreferrer">
                打开平台：{platformMeta.link}
              </a>
            }
          />

          <Card size="small" title="配置步骤">
            <Space direction="vertical" size={8} style={{ width: '100%' }}>
              {platformMeta.instructionItems.map((item) => (
                <Typography.Text key={item}>{item}</Typography.Text>
              ))}
              <Typography.Text>
                回调 URL 示例：<Typography.Text code>{platformMeta.callbackPlaceholder}</Typography.Text>
              </Typography.Text>
            </Space>
          </Card>

          <Space>
            <Button onClick={() => navigate('/im-config')}>返回平台选择</Button>
            <Button type="primary" onClick={() => setCurrentStep(1)}>
              下一步
            </Button>
          </Space>
        </Space>
      )
    }

    if (currentStep === 1) {
      return (
        <Space direction="vertical" size={16} style={{ width: '100%' }}>
          <Form form={form} layout="vertical" initialValues={credentials || { id: '', secret: '' }} style={{ maxWidth: 520 }}>
            <Form.Item
              label={platformMeta.idLabel}
              name="id"
              rules={[
                { required: true, message: `请输入 ${platformMeta.idLabel}` },
                { min: 6, message: `${platformMeta.idLabel} 长度至少 6 位` },
              ]}
            >
              <Input autoComplete="off" placeholder={`请输入 ${platformMeta.idLabel}`} />
            </Form.Item>
            <Form.Item
              label={platformMeta.secretLabel}
              name="secret"
              rules={[
                { required: true, message: `请输入 ${platformMeta.secretLabel}` },
                { min: 6, message: `${platformMeta.secretLabel} 长度至少 6 位` },
              ]}
            >
              <Input.Password autoComplete="new-password" placeholder={`请输入 ${platformMeta.secretLabel}`} />
            </Form.Item>
          </Form>

          <Space>
            <Button onClick={() => setCurrentStep(0)}>上一步</Button>
            <Button type="primary" onClick={() => void handleSubmitCredential()}>
              提交并开始配置
            </Button>
          </Space>
        </Space>
      )
    }

    if (currentStep === 2) {
      return (
        <Space direction="vertical" size={16} style={{ width: '100%' }}>
          <Typography.Text type="secondary">服务器将按顺序执行命令，请勿关闭页面。</Typography.Text>

          <Timeline
            items={configureSteps.map((step, index) => {
              const { color, dot } = configureItemStatus(step)
              return {
                color,
                dot,
                children: (
                  <Space direction="vertical" size={2} style={{ width: '100%' }}>
                    <Typography.Text>{`${index + 1}. ${step.title}`}</Typography.Text>
                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                      {step.displayCommand}
                    </Typography.Text>
                    {step.error ? <Typography.Text type="danger">{step.error}</Typography.Text> : null}
                  </Space>
                ),
              }
            })}
          />

          {configureState === 'running' ? <Alert type="info" showIcon message="正在执行自动配置，请稍候..." /> : null}

          {configureState === 'failed' ? (
            <Space direction="vertical" size={12} style={{ width: '100%' }}>
              <Alert type="error" showIcon message="自动配置失败" description={configureError || '请重试'} />
              <Space>
                <Button onClick={() => setCurrentStep(1)}>返回修改凭证</Button>
                <Button
                  type="primary"
                  onClick={() => {
                    if (!credentials) {
                      setCurrentStep(1)
                      return
                    }
                    void runConfigureFlow(credentials)
                  }}
                >
                  重新配置
                </Button>
              </Space>
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
            subTitle={verifyMessage || '每 5 秒轮询一次，最多持续 60 秒。'}
          />
        ) : null}

        {verifyState === 'success' ? (
          <Result
            status="success"
            title={`✅ ${verifyChannelName || platformMeta.name}已连接`}
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
                    返回设备详情
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

            {verifyRecord?.stderr ? <Alert type="error" showIcon message="命令错误输出" description={verifyRecord.stderr} /> : null}
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
            <Button icon={<ArrowLeftOutlined />} onClick={() => navigate('/im-config')}>
              返回平台选择
            </Button>
            <Typography.Title level={4} style={{ margin: 0 }}>
              {platformMeta.name} 配置向导
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
