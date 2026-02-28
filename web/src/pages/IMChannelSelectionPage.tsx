import { CheckCircleFilled, ClockCircleOutlined, CloseCircleFilled, ReloadOutlined } from '@ant-design/icons'
import { Alert, Button, Card, Col, Empty, Row, Select, Skeleton, Space, Tag, Typography } from 'antd'
import { useEffect, useMemo, useState } from 'react'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { DeviceOnlineTag } from '../components/DeviceOnlineTag'
import { useRealtime } from '../contexts/RealtimeContext'
import type { DeviceSnapshot } from '../types'
import { formatDateTime } from '../utils/format'

type IMPlatform = 'dingtalk' | 'feishu'

interface PlatformMeta {
  key: IMPlatform
  name: string
  description: string
}

const PLATFORM_META: PlatformMeta[] = [
  {
    key: 'dingtalk',
    name: '钉钉',
    description: '配置钉钉企业内部应用接入 OpenClaw。',
  },
  {
    key: 'feishu',
    name: '飞书',
    description: '配置飞书自建应用接入 OpenClaw。',
  },
]

function toLowerText(value?: string): string {
  return (value || '').trim().toLowerCase()
}

function statusLooksHealthy(value?: string): boolean {
  const text = toLowerText(value)
  return ['connected', 'ok', 'healthy', 'active', 'running', 'success', 'online'].some((item) => text.includes(item))
}

function statusLooksFailed(value?: string): boolean {
  const text = toLowerText(value)
  return ['error', 'failed', 'fail', 'offline', 'timeout', 'unhealthy', 'disconnected'].some((item) => text.includes(item))
}

function platformKeywords(platform: IMPlatform): string[] {
  if (platform === 'dingtalk') {
    return ['dingtalk', '钉钉', 'clawdbot-dingtalk']
  }
  return ['feishu', 'lark', '飞书', '@anthropic-ai/feishu', '@anthropic-ai/lark']
}

function describePlatformStatus(device: DeviceSnapshot | null, platform: IMPlatform): {
  tone: 'success' | 'warning' | 'default'
  text: string
} {
  const channels = device?.status?.openclaw.channels ?? []
  if (channels.length === 0) {
    return { tone: 'default', text: '未配置' }
  }

  const keywords = platformKeywords(platform)
  const matched = channels.filter((channel) => {
    const blob = `${channel.name} ${channel.status} ${channel.error}`.toLowerCase()
    return keywords.some((key) => blob.includes(key.toLowerCase()))
  })

  if (matched.length === 0) {
    return { tone: 'default', text: '未配置' }
  }

  if (matched.some((item) => statusLooksHealthy(item.status))) {
    return { tone: 'success', text: '已连接' }
  }

  if (matched.some((item) => statusLooksFailed(item.status) || Boolean(item.error))) {
    return { tone: 'warning', text: '异常' }
  }

  return { tone: 'warning', text: '已检测到通道' }
}

function statusIcon(tone: 'success' | 'warning' | 'default') {
  if (tone === 'success') {
    return <CheckCircleFilled style={{ color: '#16a34a' }} />
  }
  if (tone === 'warning') {
    return <CloseCircleFilled style={{ color: '#dc2626' }} />
  }
  return <ClockCircleOutlined style={{ color: '#6b7280' }} />
}

function statusColor(tone: 'success' | 'warning' | 'default'): 'success' | 'error' | 'default' {
  if (tone === 'success') {
    return 'success'
  }
  if (tone === 'warning') {
    return 'error'
  }
  return 'default'
}

export function IMChannelSelectionPage() {
  const navigate = useNavigate()
  const { id: routeDeviceID } = useParams()
  const [searchParams, setSearchParams] = useSearchParams()
  const requestedDeviceID = searchParams.get('deviceId') || ''

  const { devices, loading, refreshDevices } = useRealtime()
  const [selectedDeviceID, setSelectedDeviceID] = useState('')

  useEffect(() => {
    const fallback = devices.find((item) => item.online)?.id || devices[0]?.id || ''
    const initial = routeDeviceID || requestedDeviceID || fallback

    if (!initial) {
      return
    }

    const exists = devices.some((item) => item.id === initial)
    if (exists) {
      setSelectedDeviceID(initial)
      return
    }

    if (fallback) {
      setSelectedDeviceID(fallback)
    }
  }, [devices, requestedDeviceID, routeDeviceID])

  useEffect(() => {
    if (!selectedDeviceID || routeDeviceID) {
      return
    }
    setSearchParams({ deviceId: selectedDeviceID }, { replace: true })
  }, [routeDeviceID, selectedDeviceID, setSearchParams])

  const selectedDevice = useMemo(() => devices.find((item) => item.id === selectedDeviceID) || null, [devices, selectedDeviceID])

  if (loading && devices.length === 0) {
    return (
      <Card>
        <Skeleton active paragraph={{ rows: 6 }} />
      </Card>
    )
  }

  if (devices.length === 0) {
    return (
      <Card>
        <Empty description="暂无可配置设备" />
      </Card>
    )
  }

  return (
    <Space direction="vertical" size={16} style={{ width: '100%' }}>
      <Card>
        <Space style={{ width: '100%', justifyContent: 'space-between' }} wrap>
          <Typography.Title level={4} style={{ margin: 0 }}>
            IM 通道配置
          </Typography.Title>
          <Space>
            <Button icon={<ReloadOutlined />} onClick={() => void refreshDevices()} loading={loading}>
              刷新设备
            </Button>
          </Space>
        </Space>
      </Card>

      <Card>
        <Space direction="vertical" size={12} style={{ width: '100%' }}>
          <Typography.Text strong>选择要配置的设备</Typography.Text>
          <Select
            value={selectedDeviceID || undefined}
            placeholder="请选择设备"
            onChange={(value) => setSelectedDeviceID(value)}
            options={devices.map((device) => ({
              value: device.id,
              label: `${device.hostname || device.id} (${device.id})`,
            }))}
            style={{ maxWidth: 560 }}
            showSearch
            optionFilterProp="label"
          />

          {selectedDevice ? (
            <Alert
              type={selectedDevice.online ? 'success' : 'warning'}
              showIcon
              message={
                <Space>
                  <Typography.Text>{selectedDevice.hostname || selectedDevice.id}</Typography.Text>
                  <DeviceOnlineTag online={selectedDevice.online} />
                  <Typography.Text type="secondary">最后上报：{formatDateTime(selectedDevice.lastSeenAt)}</Typography.Text>
                </Space>
              }
            />
          ) : null}
        </Space>
      </Card>

      <Row gutter={[16, 16]}>
        {PLATFORM_META.map((platform) => {
          const status = describePlatformStatus(selectedDevice, platform.key)
          return (
            <Col key={platform.key} xs={24} md={12}>
              <Card
                title={platform.name}
                extra={
                  <Tag color={statusColor(status.tone)} icon={statusIcon(status.tone)}>
                    {status.text}
                  </Tag>
                }
              >
                <Space direction="vertical" size={12} style={{ width: '100%' }}>
                  <Typography.Text type="secondary">{platform.description}</Typography.Text>
                  <Button
                    type="primary"
                    disabled={!selectedDeviceID}
                    onClick={() => navigate(`/devices/${selectedDeviceID}/im-config/${platform.key}`)}
                  >
                    打开 {platform.name} 向导
                  </Button>
                </Space>
              </Card>
            </Col>
          )
        })}
      </Row>
    </Space>
  )
}
