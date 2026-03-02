import { ReloadOutlined, SettingOutlined } from '@ant-design/icons'
import {
  Alert,
  Button,
  Card,
  Form,
  Input,
  Modal,
  Select,
  Space,
  Tag,
  Typography,
  message,
} from 'antd'
import { useCallback, useEffect, useState } from 'react'

const API_BASE = '/api'

async function apiRequest<T>(path: string, token: string | null, opts?: RequestInit): Promise<T> {
  const res = await fetch(API_BASE + path, {
    ...opts,
    headers: { ...(opts?.headers as Record<string, string> || {}), ...(token ? { Authorization: 'Bearer ' + token } : {}) },
  })
  if (!res.ok) throw new Error(await res.text())
  return res.json() as Promise<T>
}

interface EmbeddingConfig {
  provider: string
  model: string
  apiKey: string
  baseUrl: string
}

interface EmbeddingFormValues {
  provider: string
  model: string
  apiKey: string
  baseUrl: string
}

const PROVIDER_OPTIONS = [
  { label: 'OpenAI', value: 'openai' },
  { label: 'Ollama (local)', value: 'ollama' },
  { label: 'Voyage AI', value: 'voyage' },
  { label: 'Custom', value: 'custom' },
]

const PROVIDER_DEFAULTS: Record<string, Partial<EmbeddingFormValues>> = {
  openai: { baseUrl: 'https://api.openai.com/v1', model: 'text-embedding-3-small' },
  ollama: { baseUrl: 'http://localhost:11434', model: 'nomic-embed-text' },
  voyage: { baseUrl: 'https://api.voyageai.com/v1', model: 'voyage-3-lite' },
  custom: { baseUrl: '', model: '' },
}

interface Props {
  token: string | null
  deviceId: string
  online: boolean
}

export default function EmbeddingProviderCard({ token, deviceId, online }: Props) {
  const [config, setConfig] = useState<EmbeddingConfig | null>(null)
  const [loading, setLoading] = useState(false)
  const [modalOpen, setModalOpen] = useState(false)
  const [saving, setSaving] = useState(false)
  const [indexing, setIndexing] = useState(false)
  const [form] = Form.useForm<EmbeddingFormValues>()

  const fetchConfig = useCallback(async () => {
    if (!online) return
    setLoading(true)
    try {
      const data = await apiRequest<{ embedding?: EmbeddingConfig; error?: string }>(
        `/devices/${deviceId}/embedding-provider`,
        token,
      )
      if (data && !data.error && data.embedding) {
        setConfig(data.embedding)
      } else {
        setConfig(null)
      }
    } catch {
      // silent
    } finally {
      setLoading(false)
    }
  }, [token, deviceId, online])

  useEffect(() => {
    void fetchConfig()
  }, [fetchConfig])

  const openConfigure = () => {
    const defaults: EmbeddingFormValues = config
      ? { provider: config.provider, model: config.model, apiKey: '', baseUrl: config.baseUrl }
      : { provider: 'openai', model: 'text-embedding-3-small', apiKey: '', baseUrl: 'https://api.openai.com/v1' }
    form.setFieldsValue(defaults)
    setModalOpen(true)
  }

  const handleProviderChange = (provider: string) => {
    const defaults = PROVIDER_DEFAULTS[provider] || {}
    form.setFieldsValue({ ...defaults, apiKey: form.getFieldValue('apiKey') })
  }

  const handleSave = async () => {
    try {
      const values = await form.validateFields()
      setSaving(true)
      await apiRequest(`/devices/${deviceId}/embedding-provider`, token, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(values),
      })
      message.success('Embedding 配置已保存')
      setModalOpen(false)
      void fetchConfig()
      // Auto-trigger memory index
      triggerMemoryIndex()
    } catch (err: unknown) {
      if (err && typeof err === 'object' && 'errorFields' in err) return
      message.error('保存失败')
    } finally {
      setSaving(false)
    }
  }

  const triggerMemoryIndex = async () => {
    setIndexing(true)
    try {
      await apiRequest(`/devices/${deviceId}/exec`, token, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ command: 'openclaw', args: ['memory', 'index', '--force'], timeout: 120 }),
      })
      message.success('Memory index 已触发')
    } catch {
      message.warning('Memory index 触发失败，请稍后手动执行')
    } finally {
      setIndexing(false)
    }
  }

  const configured = config && config.provider
  const selectedProvider = Form.useWatch('provider', form)

  return (
    <Card
      title="Embedding 配置"
      extra={
        <Space>
          <Button icon={<ReloadOutlined />} onClick={() => void fetchConfig()} loading={loading} disabled={!online} size="small">
            刷新
          </Button>
          <Button type="primary" icon={<SettingOutlined />} disabled={!online} size="small" onClick={openConfigure}>
            配置
          </Button>
        </Space>
      }
    >
      {!online && <Alert type="warning" showIcon message="设备离线，无法管理 Embedding 配置" style={{ marginBottom: 12 }} />}

      <Space direction="vertical" size={8} style={{ width: '100%' }}>
        <Space>
          <span style={{ fontSize: 18 }}>{configured ? '🟢' : '🔴'}</span>
          <Typography.Text strong>
            {configured ? '已配置' : '未配置'}
          </Typography.Text>
          {configured && (
            <>
              <Tag color="blue">{config.provider}</Tag>
              <Tag>{config.model}</Tag>
            </>
          )}
        </Space>
        {configured && config.baseUrl && (
          <Typography.Text type="secondary">Base URL: {config.baseUrl}</Typography.Text>
        )}
        {configured && config.apiKey && (
          <Typography.Text type="secondary">API Key: {config.apiKey}</Typography.Text>
        )}
        {indexing && <Typography.Text type="secondary">正在重建 Memory Index...</Typography.Text>}
      </Space>

      <Modal
        title="配置 Embedding Provider"
        open={modalOpen}
        onCancel={() => setModalOpen(false)}
        onOk={() => void handleSave()}
        confirmLoading={saving}
        okText="保存"
        cancelText="取消"
        destroyOnClose
      >
        <Form form={form} layout="vertical" style={{ marginTop: 16 }}>
          <Form.Item name="provider" label="Provider" rules={[{ required: true, message: '请选择 Provider' }]}>
            <Select options={PROVIDER_OPTIONS} onChange={handleProviderChange} />
          </Form.Item>
          <Form.Item name="model" label="Model 名称" rules={[{ required: true, message: '请输入 Model 名称' }]}>
            <Input placeholder="text-embedding-3-small" />
          </Form.Item>
          <Form.Item
            name="apiKey"
            label="API Key"
            rules={[{ required: !config, message: '请输入 API Key' }]}
            extra={selectedProvider === 'ollama' ? '本地 Ollama 无需 API Key，可留空' : (config ? '留空保持原值' : '')}
          >
            <Input.Password placeholder={config ? '留空保持原值' : 'sk-...'} />
          </Form.Item>
          <Form.Item name="baseUrl" label="Base URL" rules={[{ required: true, message: '请输入 Base URL' }]}>
            <Input placeholder="https://api.openai.com/v1" />
          </Form.Item>
        </Form>
      </Modal>
    </Card>
  )
}
