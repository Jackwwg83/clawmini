import { CopyOutlined, DeleteOutlined } from '@ant-design/icons'
import {
  Button,
  Card,
  Form,
  Input,
  Modal,
  Skeleton,
  Select,
  Space,
  Table,
  Tag,
  Typography,
  message,
} from 'antd'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { createJoinToken, deleteJoinToken, listJoinTokens } from '../api/client'
import { useAuth } from '../contexts/AuthContext'
import type { JoinToken } from '../types'
import { formatDateTime } from '../utils/format'

interface CreateTokenForm {
  label?: string
  expiresInHours: number
}

function maskJoinToken(token: string): string {
  if (!token) {
    return '--'
  }
  if (token.length <= 10) {
    return `${token.slice(0, 2)}****`
  }
  return `${token.slice(0, 6)}****${token.slice(-4)}`
}

function tokenStatus(token: JoinToken): { text: string; color: string } {
  const now = Math.floor(Date.now() / 1000)
  if (token.usedAt) {
    return { text: '已使用', color: 'success' }
  }
  if (token.expiresAt <= now) {
    return { text: '已过期', color: 'error' }
  }
  return { text: '未使用', color: 'processing' }
}

function installBaseURL(): string {
  const host = import.meta.env.DEV ? 'localhost:18790' : window.location.host
  const protocol = import.meta.env.DEV ? 'http:' : window.location.protocol
  return `${protocol}//${host}`
}

export function OnboardingPage() {
  const [form] = Form.useForm<CreateTokenForm>()
  const { token } = useAuth()
  const [tokens, setTokens] = useState<JoinToken[]>([])
  const [loading, setLoading] = useState(false)
  const [creating, setCreating] = useState(false)
  const [deletingID, setDeletingID] = useState<string | null>(null)
  const [latestToken, setLatestToken] = useState<JoinToken | null>(null)

  const loadTokens = useCallback(async () => {
    if (!token) {
      setTokens([])
      return
    }

    setLoading(true)
    try {
      const list = await listJoinTokens(token)
      setTokens(list)
    } catch (err) {
      if (err instanceof Error) {
        message.error(err.message || '加载接入令牌失败')
      } else {
        message.error('加载接入令牌失败')
      }
    } finally {
      setLoading(false)
    }
  }, [token])

  useEffect(() => {
    void loadTokens()
  }, [loadTokens])

  const handleCreateToken = async (values: CreateTokenForm) => {
    if (!token) {
      return
    }

    setCreating(true)
    try {
      const created = await createJoinToken(token, (values.label || '').trim(), values.expiresInHours)
      setLatestToken(created)
      message.success('接入令牌已生成')
      form.setFieldsValue({ label: '', expiresInHours: values.expiresInHours })
      await loadTokens()
    } catch (err) {
      if (err instanceof Error) {
        message.error(err.message || '生成接入令牌失败')
      } else {
        message.error('生成接入令牌失败')
      }
    } finally {
      setCreating(false)
    }
  }

  const handleDeleteToken = (target: JoinToken) => {
    if (!token) {
      return
    }

    Modal.confirm({
      title: '删除令牌',
      content: '确定要删除此接入令牌吗？',
      okText: '删除',
      okType: 'danger',
      cancelText: '取消',
      onOk: async () => {
        setDeletingID(target.id)
        try {
          await deleteJoinToken(token, target.id)
          message.success('令牌已删除')
          if (latestToken?.id === target.id) {
            setLatestToken(null)
          }
          await loadTokens()
        } catch (err) {
          if (err instanceof Error) {
            message.error(err.message || '删除令牌失败')
          } else {
            message.error('删除令牌失败')
          }
        } finally {
          setDeletingID(null)
        }
      },
    })
  }

  const installCommand = useMemo(() => {
    if (!latestToken) {
      return ''
    }
    const base = installBaseURL()
    return `curl -fsSL ${base}/install.sh?token=${encodeURIComponent(latestToken.id)} | sudo bash`
  }, [latestToken])

  const columns = [
    {
      title: '标签',
      dataIndex: 'label',
      key: 'label',
      render: (value: string) => value || '--',
    },
    {
      title: '令牌',
      dataIndex: 'id',
      key: 'id',
      render: (value: string) => <Typography.Text code>{maskJoinToken(value)}</Typography.Text>,
    },
    {
      title: '创建时间',
      dataIndex: 'createdAt',
      key: 'createdAt',
      render: (value: number) => formatDateTime(value),
    },
    {
      title: '过期时间',
      dataIndex: 'expiresAt',
      key: 'expiresAt',
      render: (value: number) => formatDateTime(value),
    },
    {
      title: '状态',
      key: 'status',
      render: (_: unknown, record: JoinToken) => {
        const status = tokenStatus(record)
        return <Tag color={status.color}>{status.text}</Tag>
      },
    },
    {
      title: '操作',
      key: 'actions',
      render: (_: unknown, record: JoinToken) => (
        <Button
          type="link"
          danger
          icon={<DeleteOutlined />}
          loading={deletingID === record.id}
          onClick={() => handleDeleteToken(record)}
        >
          删除
        </Button>
      ),
    },
  ]

  return (
    <Space direction="vertical" size={16} style={{ width: '100%' }}>
      <Card>
        <Typography.Title level={4} style={{ marginBottom: 8 }}>
          设备接入
        </Typography.Title>
        <Typography.Text type="secondary">
          生成一次性接入令牌并分发安装命令，新设备执行后将自动接入当前平台。
        </Typography.Text>
      </Card>

      <Card title="生成接入令牌">
        <Form<CreateTokenForm>
          form={form}
          layout="inline"
          initialValues={{ label: '', expiresInHours: 24 }}
          onFinish={(values) => void handleCreateToken(values)}
        >
          <Form.Item name="label" label="标签">
            <Input placeholder="例如：北京机房-01" style={{ width: 260 }} allowClear />
          </Form.Item>
          <Form.Item name="expiresInHours" label="有效期" rules={[{ required: true, message: '请选择有效期' }]}>
            <Select
              style={{ width: 150 }}
              options={[
                { label: '1 小时', value: 1 },
                { label: '6 小时', value: 6 },
                { label: '24 小时', value: 24 },
                { label: '7 天', value: 168 },
              ]}
            />
          </Form.Item>
          <Form.Item>
            <Button type="primary" htmlType="submit" loading={creating}>
              生成令牌
            </Button>
          </Form.Item>
        </Form>

        {latestToken ? (
          <Space direction="vertical" size={10} style={{ width: '100%', marginTop: 16 }}>
            <Typography.Text strong>最新令牌：{latestToken.id}</Typography.Text>
            <Typography.Paragraph code style={{ marginBottom: 0 }}>
              {installCommand}
            </Typography.Paragraph>
            <Button
              icon={<CopyOutlined />}
              onClick={() => {
                void navigator.clipboard
                  .writeText(installCommand)
                  .then(() => message.success('安装命令已复制'))
                  .catch(() => message.error('复制失败，请手动复制'))
              }}
            >
              复制安装命令
            </Button>
          </Space>
        ) : null}
      </Card>

      <Card title="已生成的令牌">
        {loading && tokens.length === 0 ? (
          <Skeleton active paragraph={{ rows: 6 }} />
        ) : (
          <Table<JoinToken>
            rowKey="id"
            loading={loading}
            columns={columns}
            dataSource={tokens}
            pagination={{ pageSize: 8, showSizeChanger: false }}
          />
        )}
      </Card>
    </Space>
  )
}
