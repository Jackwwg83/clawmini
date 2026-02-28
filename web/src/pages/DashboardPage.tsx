import {
  ArrowRightOutlined,
  DatabaseOutlined,
  DesktopOutlined,
  MessageOutlined,
  PlusOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons'
import {
  Button,
  Card,
  Col,
  Empty,
  Progress,
  Row,
  Space,
  Spin,
  Statistic,
  Typography,
} from 'antd'
import { useMemo } from 'react'
import { useNavigate } from 'react-router-dom'
import { DeviceOnlineTag } from '../components/DeviceOnlineTag'
import { useRealtime } from '../contexts/RealtimeContext'
import { formatBytes, formatDateTime, formatLastSeen, formatPercent, toProgress } from '../utils/format'

export function DashboardPage() {
  const navigate = useNavigate()
  const { devices, loading, refreshDevices } = useRealtime()

  const stats = useMemo(() => {
    const total = devices.length
    const online = devices.filter((item) => item.online).length
    const offline = total - online
    return { total, online, offline }
  }, [devices])

  return (
    <Space direction="vertical" size={16} style={{ width: '100%' }}>
      <Row gutter={[16, 16]}>
        <Col xs={24} md={8}>
          <Card>
            <Statistic title="设备总数" value={stats.total} prefix={<DesktopOutlined />} />
          </Card>
        </Col>
        <Col xs={24} md={8}>
          <Card>
            <Statistic title="在线设备" value={stats.online} valueStyle={{ color: '#059669' }} />
          </Card>
        </Col>
        <Col xs={24} md={8}>
          <Card>
            <Statistic title="离线设备" value={stats.offline} valueStyle={{ color: '#9ca3af' }} />
          </Card>
        </Col>
      </Row>

      <Card
        title="设备列表"
        extra={
          <Space>
            <Button icon={<MessageOutlined />} onClick={() => navigate('/im-config')}>
              IM 配置
            </Button>
            <Button type="primary" icon={<PlusOutlined />} onClick={() => navigate('/onboarding')}>
              接入设备
            </Button>
            <Button onClick={() => void refreshDevices()} loading={loading}>
              刷新
            </Button>
          </Space>
        }
      >
        {loading ? (
          <div className="center-block">
            <Spin />
          </div>
        ) : null}

        {!loading && devices.length === 0 ? (
          <Empty description="暂无设备数据" />
        ) : null}

        <Row gutter={[16, 16]}>
          {devices.map((device) => {
            const memPercent = toProgress(device.status?.memUsed ?? 0, device.status?.memTotal ?? 0)
            const diskPercent = toProgress(device.status?.diskUsed ?? 0, device.status?.diskTotal ?? 0)

            return (
              <Col key={device.id} xs={24} md={12} xl={8}>
                <Card
                  className="device-card"
                  hoverable
                  title={
                    <Space>
                      <Typography.Text strong>{device.hostname || device.id}</Typography.Text>
                      <DeviceOnlineTag online={device.online} />
                    </Space>
                  }
                  extra={
                    <Space>
                      <Button type="link" icon={<MessageOutlined />} onClick={() => navigate(`/devices/${device.id}/im-config`)}>
                        IM 配置
                      </Button>
                      <Button
                        type="link"
                        icon={<ArrowRightOutlined />}
                        onClick={() => navigate(`/devices/${device.id}`)}
                      >
                        查看详情
                      </Button>
                    </Space>
                  }
                >
                  <Space direction="vertical" size={10} style={{ width: '100%' }}>
                    <Typography.Text type="secondary">
                      {device.os} / {device.arch}
                    </Typography.Text>

                    <div>
                      <Space style={{ width: '100%', justifyContent: 'space-between' }}>
                        <span>
                          <ThunderboltOutlined /> CPU
                        </span>
                        <Typography.Text>{formatPercent(device.status?.cpuUsage)}</Typography.Text>
                      </Space>
                      <Progress percent={device.status?.cpuUsage ?? 0} size="small" showInfo={false} />
                    </div>

                    <div>
                      <Space style={{ width: '100%', justifyContent: 'space-between' }}>
                        <span>
                          <DatabaseOutlined /> 内存
                        </span>
                        <Typography.Text>
                          {formatBytes(device.status?.memUsed ?? 0)} / {formatBytes(device.status?.memTotal ?? 0)}
                        </Typography.Text>
                      </Space>
                      <Progress percent={memPercent} size="small" showInfo={false} strokeColor="#0ea5a4" />
                    </div>

                    <div>
                      <Space style={{ width: '100%', justifyContent: 'space-between' }}>
                        <span>
                          <DatabaseOutlined /> 磁盘
                        </span>
                        <Typography.Text>
                          {formatBytes(device.status?.diskUsed ?? 0)} /{' '}
                          {formatBytes(device.status?.diskTotal ?? 0)}
                        </Typography.Text>
                      </Space>
                      <Progress percent={diskPercent} size="small" showInfo={false} strokeColor="#f59e0b" />
                    </div>

                    <Typography.Text>
                      OpenClaw 版本：
                      {device.status?.openclaw.version || device.openclawVersion || '--'}
                    </Typography.Text>
                    <Typography.Text type="secondary">
                      最后心跳：{formatLastSeen(device.lastSeenAt)}（{formatDateTime(device.lastSeenAt)}）
                    </Typography.Text>
                  </Space>
                </Card>
              </Col>
            )
          })}
        </Row>
      </Card>
    </Space>
  )
}
