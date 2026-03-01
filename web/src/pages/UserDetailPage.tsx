import { ArrowLeftOutlined, LinkOutlined } from '@ant-design/icons'
import { Button, Card, Descriptions, Empty, Modal, Select, Space, Table, Tag, Typography, message } from 'antd'
import { useEffect, useMemo, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { bindUserDevice, fetchDevices, fetchUserById, unbindUserDevice } from '../api/client'
import { useAuth } from '../contexts/AuthContext'
import type { DeviceSnapshot, User } from '../types'
import { formatDateTime } from '../utils/format'

export function UserDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const { token } = useAuth()
  const [user, setUser] = useState<User | null>(null)
  const [boundDevices, setBoundDevices] = useState<DeviceSnapshot[]>([])
  const [allDevices, setAllDevices] = useState<DeviceSnapshot[]>([])
  const [loading, setLoading] = useState(false)
  const [bindOpen, setBindOpen] = useState(false)
  const [bindingDeviceId, setBindingDeviceId] = useState<string>('')

  const loadDetail = async () => {
    if (!token || !id) {
      return
    }
    setLoading(true)
    try {
      const [detail, devices] = await Promise.all([fetchUserById(token, id), fetchDevices(token)])
      setUser(detail.user)
      setBoundDevices(detail.devices)
      setAllDevices(devices)
    } catch (err) {
      message.error(err instanceof Error ? err.message : '加载用户详情失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void loadDetail()
  }, [id, token])

  const boundDeviceIds = useMemo(() => new Set(boundDevices.map((device) => device.id)), [boundDevices])
  const bindOptions = useMemo(
    () => allDevices.filter((device) => !boundDeviceIds.has(device.id)).map((device) => ({ value: device.id, label: `${device.hostname || device.id} (${device.id})` })),
    [allDevices, boundDeviceIds],
  )

  const onBindDevice = async () => {
    if (!token || !id || !bindingDeviceId) {
      return
    }
    try {
      await bindUserDevice(token, id, bindingDeviceId)
      message.success('绑定成功')
      setBindOpen(false)
      setBindingDeviceId('')
      await loadDetail()
    } catch (err) {
      message.error(err instanceof Error ? err.message : '绑定失败')
    }
  }

  return (
    <Space direction="vertical" size={16} style={{ width: '100%' }}>
      <Button icon={<ArrowLeftOutlined />} onClick={() => navigate('/users')}>
        返回用户列表
      </Button>

      <Card loading={loading} title="用户详情">
        {user ? (
          <Descriptions column={2} bordered>
            <Descriptions.Item label="用户名">{user.username}</Descriptions.Item>
            <Descriptions.Item label="显示名">{user.displayName}</Descriptions.Item>
            <Descriptions.Item label="角色">
              <Tag color={user.role === 'admin' ? 'gold' : 'blue'}>{user.role}</Tag>
            </Descriptions.Item>
            <Descriptions.Item label="创建时间">{formatDateTime(user.createdAt)}</Descriptions.Item>
          </Descriptions>
        ) : (
          <Empty description="用户不存在" />
        )}
      </Card>

      <Card
        title="已绑定设备"
        extra={
          <Button icon={<LinkOutlined />} type="primary" onClick={() => setBindOpen(true)}>
            绑定设备
          </Button>
        }
      >
        <Table<DeviceSnapshot>
          rowKey="id"
          loading={loading}
          dataSource={boundDevices}
          locale={{ emptyText: <Typography.Text type="secondary">暂无绑定设备</Typography.Text> }}
          columns={[
            {
              title: '设备',
              dataIndex: 'hostname',
              key: 'hostname',
              render: (_: string, record) => record.hostname || record.id,
            },
            {
              title: 'ID',
              dataIndex: 'id',
              key: 'id',
            },
            {
              title: '状态',
              dataIndex: 'online',
              key: 'online',
              width: 100,
              render: (online: boolean) => <Tag color={online ? 'green' : 'default'}>{online ? '在线' : '离线'}</Tag>,
            },
            {
              title: '操作',
              key: 'actions',
              width: 120,
              render: (_, record) => (
                <Button
                  danger
                  type="link"
                  onClick={async () => {
                    if (!token || !id) {
                      return
                    }
                    try {
                      await unbindUserDevice(token, id, record.id)
                      message.success('解绑成功')
                      await loadDetail()
                    } catch (err) {
                      message.error(err instanceof Error ? err.message : '解绑失败')
                    }
                  }}
                >
                  解绑
                </Button>
              ),
            },
          ]}
        />
      </Card>

      <Modal
        title="绑定设备"
        open={bindOpen}
        onOk={() => void onBindDevice()}
        onCancel={() => {
          setBindOpen(false)
          setBindingDeviceId('')
        }}
      >
        <Select
          style={{ width: '100%' }}
          placeholder="选择设备"
          options={bindOptions}
          value={bindingDeviceId || undefined}
          onChange={(value) => setBindingDeviceId(value)}
        />
      </Modal>
    </Space>
  )
}
