import { DeleteOutlined, EditOutlined, PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import {
  Alert,
  Button,
  Card,
  Empty,
  Form,
  Input,
  List,
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
    headers: { ...(opts?.headers as Record<string,string> || {}), ...(token ? { Authorization: 'Bearer ' + token } : {}) },
  })
  if (!res.ok) throw new Error(await res.text())
  return res.json() as Promise<T>
}

interface ProviderInfo {
  name: string
  baseUrl: string
  apiKey: string
  apiType: string
  models?: number
}

interface ProviderFormValues {
  name: string
  baseUrl: string
  apiKey: string
  apiType: string
}

const API_TYPE_OPTIONS = [
  { label: 'Anthropic Messages', value: 'anthropic-messages' },
  { label: 'OpenAI Chat', value: 'openai-chat' },
  { label: 'OpenAI Responses', value: 'openai-responses' },
]

const PRESETS: Record<string, Partial<ProviderFormValues>> = {
  anthropic: { name: 'anthropic', baseUrl: 'https://api.anthropic.com', apiType: 'anthropic-messages' },
  openai: { name: 'openai', baseUrl: 'https://api.openai.com/v1', apiType: 'openai-responses' },
}

interface Props {
  token: string | null
  deviceId: string
  online: boolean
}

export default function ModelProvidersCard({ token, deviceId, online }: Props) {
  const [providers, setProviders] = useState<ProviderInfo[]>([])
  const [loading, setLoading] = useState(false)
  const [modalOpen, setModalOpen] = useState(false)
  const [editing, setEditing] = useState<ProviderInfo | null>(null)
  const [saving, setSaving] = useState(false)
  const [form] = Form.useForm<ProviderFormValues>()

  const fetchProviders = useCallback(async () => {
    if (!online) return
    setLoading(true)
    try {
      const data = await apiRequest<Record<string, { baseUrl: string; apiKey: string; apiType: string; models: number }>>(
        `/devices/${deviceId}/model-providers`,
        token || "",
      )
      const list = Object.entries(data).map(([name, val]) => {
        const info = val as { baseUrl: string; apiKey: string; apiType: string; models: number }
        return {
          name,
          baseUrl: info.baseUrl || '',
          apiKey: info.apiKey || '',
          apiType: info.apiType || '',
          models: info.models ?? 0,
        }
      })
      setProviders(list)
    } catch {
      // silent
    } finally {
      setLoading(false)
    }
  }, [token, deviceId, online])

  useEffect(() => {
    void fetchProviders()
  }, [fetchProviders])

  const openAdd = (preset?: string) => {
    const values = preset && PRESETS[preset] ? { ...PRESETS[preset], apiKey: '' } : { name: '', baseUrl: '', apiKey: '', apiType: 'openai-chat' }
    form.setFieldsValue(values as ProviderFormValues)
    setEditing(null)
    setModalOpen(true)
  }

  const openEdit = (provider: ProviderInfo) => {
    form.setFieldsValue({ name: provider.name, baseUrl: provider.baseUrl, apiKey: '', apiType: provider.apiType })
    setEditing(provider)
    setModalOpen(true)
  }

  const handleSave = async () => {
    try {
      const values = await form.validateFields()
      setSaving(true)
      await apiRequest(`/devices/${deviceId}/model-providers`, token, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(values),
      })
      message.success('Provider 已保存')
      setModalOpen(false)
      void fetchProviders()
    } catch (err: unknown) {
      if (err && typeof err === 'object' && 'errorFields' in err) return
      message.error('保存失败')
    } finally {
      setSaving(false)
    }
  }

  const handleDelete = async (name: string) => {
    Modal.confirm({
      title: '删除 Provider',
      content: `确定删除 "${name}" 吗？`,
      okText: '删除',
      okType: 'danger',
      cancelText: '取消',
      onOk: async () => {
        try {
          await apiRequest(`/devices/${deviceId}/model-providers/${name}`, token, { method: 'DELETE' })
          message.success('已删除')
          void fetchProviders()
        } catch {
          message.error('删除失败')
        }
      },
    })
  }

  const apiTypeLabel = (type: string) => API_TYPE_OPTIONS.find((o) => o.value === type)?.label || type

  return (
    <Card
      title="模型配置"
      extra={
        <Space>
          <Button icon={<ReloadOutlined />} onClick={() => void fetchProviders()} loading={loading} disabled={!online} size="small">
            刷新
          </Button>
          <Button type="primary" icon={<PlusOutlined />} disabled={!online} size="small" onClick={() => openAdd()}>
            添加 Provider
          </Button>
        </Space>
      }
    >
      {!online && <Alert type="warning" showIcon message="设备离线，无法管理模型配置" style={{ marginBottom: 12 }} />}

      {providers.length > 0 ? (
        <List
          dataSource={providers}
          renderItem={(p) => (
            <List.Item
              actions={[
                <Button key="edit" type="link" icon={<EditOutlined />} onClick={() => openEdit(p)} disabled={!online}>编辑</Button>,
                <Button key="delete" type="link" danger icon={<DeleteOutlined />} onClick={() => void handleDelete(p.name)} disabled={!online}>删除</Button>,
              ]}
            >
              <List.Item.Meta
                title={<Space><Typography.Text strong>{p.name}</Typography.Text><Tag color="blue">{apiTypeLabel(p.apiType)}</Tag>{p.models ? <Tag>{p.models} 模型</Tag> : null}</Space>}
                description={<Space direction="vertical" size={0}><Typography.Text type="secondary">API: {p.baseUrl}</Typography.Text><Typography.Text type="secondary">Key: {p.apiKey}</Typography.Text></Space>}
              />
            </List.Item>
          )}
        />
      ) : (
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={loading ? '加载中...' : '暂无 Provider 配置'}>
          {!loading && online && (
            <Space>
              <Button onClick={() => openAdd('anthropic')}>+ Anthropic</Button>
              <Button onClick={() => openAdd('openai')}>+ OpenAI</Button>
              <Button onClick={() => openAdd()}>+ 自定义</Button>
            </Space>
          )}
        </Empty>
      )}

      <Modal
        title={editing ? `编辑 ${editing.name}` : '添加 Provider'}
        open={modalOpen}
        onCancel={() => setModalOpen(false)}
        onOk={() => void handleSave()}
        confirmLoading={saving}
        okText="保存"
        cancelText="取消"
        destroyOnClose
      >
        <Form form={form} layout="vertical" style={{ marginTop: 16 }}>
          <Form.Item name="name" label="Provider 名称" rules={[{ required: true, message: '请输入名称' }, { pattern: /^[a-zA-Z0-9_-]+$/, message: '仅支持字母、数字、下划线和连字符' }]}>
            <Input placeholder="如 anthropic, openai, my-proxy" disabled={!!editing} />
          </Form.Item>
          <Form.Item name="baseUrl" label="API 地址" rules={[{ required: true, message: '请输入 API 地址' }]}>
            <Input placeholder="https://api.anthropic.com" />
          </Form.Item>
          <Form.Item name="apiKey" label="API 密钥" rules={[{ required: !editing, message: '请输入 API Key' }]}>
            <Input.Password placeholder={editing ? '留空保持原值' : 'sk-...'} />
          </Form.Item>
          <Form.Item name="apiType" label="API 类型" rules={[{ required: true, message: '请选择 API 类型' }]}>
            <Select options={API_TYPE_OPTIONS} />
          </Form.Item>
        </Form>
      </Modal>
    </Card>
  )
}
