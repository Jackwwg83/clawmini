import {
  ArrowRightOutlined,
  DatabaseOutlined,
  DesktopOutlined,
  ExclamationCircleOutlined,
  MessageOutlined,
  PlusOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons'
import {
  Alert,
  Button,
  Card,
  Checkbox,
  Col,
  Empty,
  Modal,
  Progress,
  Row,
  Skeleton,
  Space,
  Statistic,
  Table,
  Tag,
  Typography,
  message,
} from 'antd'
import { useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { fetchBatchJob, startBatchExec } from '../api/client'
import { DeviceOnlineTag } from '../components/DeviceOnlineTag'
import { useAuth } from '../contexts/AuthContext'
import { useRealtime } from '../contexts/RealtimeContext'
import type { BatchJob, BatchJobItem } from '../types'
import { formatBytes, formatDateTime, formatLastSeen, formatPercent, toProgress } from '../utils/format'

const BATCH_ACTIONS = [
  { key: 'restart-gateway', label: 'Restart Gateway', command: 'openclaw gateway restart' },
  { key: 'run-doctor', label: 'Run Doctor', command: 'openclaw doctor --json' },
  { key: 'check-updates', label: 'Check Updates', command: 'openclaw update status --json' },
  { key: 'update-all', label: 'Update All', command: 'openclaw update --json' },
]

function hasAttentionIssue(gatewayStatus?: string): boolean {
  const text = (gatewayStatus || '').trim().toLowerCase()
  return ['error', 'fail', 'unhealthy', 'offline', 'down', 'inactive'].some((word) => text.includes(word))
}

function batchStatusTag(status: string) {
  if (status === 'success') {
    return <Tag color="success">成功</Tag>
  }
  if (status === 'error') {
    return <Tag color="error">失败</Tag>
  }
  if (status === 'running') {
    return <Tag color="processing">执行中</Tag>
  }
  if (status === 'pending') {
    return <Tag color="default">等待中</Tag>
  }
  return <Tag>{status || '未知'}</Tag>
}

export function DashboardPage() {
  const navigate = useNavigate()
  const { token } = useAuth()
  const { devices, loading, refreshDevices } = useRealtime()
  const [selectedDeviceIDs, setSelectedDeviceIDs] = useState<string[]>([])
  const [batchModalOpen, setBatchModalOpen] = useState(false)
  const [batchLoading, setBatchLoading] = useState(false)
  const [batchJob, setBatchJob] = useState<BatchJob | null>(null)

  const stats = useMemo(() => {
    const total = devices.length
    const online = devices.filter((item) => item.online).length
    const needsAttention = devices.filter((device) => {
      if (!device.online || !device.hasOpenClaw) {
        return true
      }
      return hasAttentionIssue(device.status?.openclaw.gatewayStatus)
    }).length
    return { total, online, needsAttention }
  }, [devices])

  useEffect(() => {
    setSelectedDeviceIDs((prev) => prev.filter((id) => devices.some((device) => device.id === id)))
  }, [devices])

  useEffect(() => {
    if (!token || !batchModalOpen || !batchJob) {
      return
    }
    if (batchJob.status !== 'queued' && batchJob.status !== 'running') {
      return
    }

    const timer = window.setTimeout(() => {
      fetchBatchJob(token, batchJob.id)
        .then((nextJob) => {
          setBatchJob(nextJob)
          if (nextJob.status === 'success') {
            message.success('批量任务执行完成')
            void refreshDevices()
          } else if (nextJob.status === 'failed') {
            message.warning('批量任务部分失败，请查看明细')
            void refreshDevices()
          }
        })
        .catch((err) => {
          message.error(err instanceof Error ? err.message : '获取批量任务状态失败')
        })
    }, 1200)

    return () => window.clearTimeout(timer)
  }, [batchJob, batchModalOpen, refreshDevices, token])

  const selectedSet = useMemo(() => new Set(selectedDeviceIDs), [selectedDeviceIDs])

  const batchProgress = useMemo(() => {
    if (!batchJob || batchJob.totalCount <= 0) {
      return 0
    }
    return Math.round(((batchJob.successCount + batchJob.errorCount) / batchJob.totalCount) * 100)
  }, [batchJob])

  const itemColumns = [
    {
      title: '设备',
      dataIndex: 'deviceId',
      key: 'deviceId',
      render: (value: string) => devices.find((device) => device.id === value)?.hostname || value,
    },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      render: (value: string) => batchStatusTag(value),
      width: 120,
    },
    {
      title: '错误',
      dataIndex: 'error',
      key: 'error',
      render: (value?: string) => value || '--',
    },
  ]

  const triggerBatchAction = async (command: string, label: string) => {
    if (!token || selectedDeviceIDs.length === 0) {
      return
    }

    setBatchLoading(true)
    try {
      const created = await startBatchExec(token, {
        command,
        deviceIds: selectedDeviceIDs,
      })
      setBatchJob(created.job)
      setBatchModalOpen(true)
      message.success(`${label} 任务已创建`)
    } catch (err) {
      message.error(err instanceof Error ? err.message : '创建批量任务失败')
    } finally {
      setBatchLoading(false)
    }
  }

  const loadingSkeleton = (
    <Space direction="vertical" size={12} style={{ width: '100%' }}>
      <Skeleton active paragraph={{ rows: 2 }} />
      <Skeleton active paragraph={{ rows: 2 }} />
      <Skeleton active paragraph={{ rows: 2 }} />
    </Space>
  )

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
            <Statistic title="需关注设备" value={stats.needsAttention} valueStyle={{ color: '#d97706' }} prefix={<ExclamationCircleOutlined />} />
          </Card>
        </Col>
      </Row>

      {selectedDeviceIDs.length > 0 ? (
        <Alert
          type="info"
          showIcon
          message={
            <Space wrap>
              <Typography.Text>已选择 {selectedDeviceIDs.length} 台设备</Typography.Text>
              {BATCH_ACTIONS.map((action) => (
                <Button
                  key={action.key}
                  onClick={() => void triggerBatchAction(action.command, action.label)}
                  loading={batchLoading}
                >
                  {action.label}
                </Button>
              ))}
              <Button onClick={() => setSelectedDeviceIDs([])}>清空选择</Button>
            </Space>
          }
        />
      ) : null}

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
        {loading && devices.length === 0 ? loadingSkeleton : null}

        {!loading && devices.length === 0 ? <Empty description="暂无设备数据" /> : null}

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
                      <Checkbox
                        checked={selectedSet.has(device.id)}
                        onChange={(event) => {
                          if (event.target.checked) {
                            setSelectedDeviceIDs((prev) => [...prev, device.id])
                            return
                          }
                          setSelectedDeviceIDs((prev) => prev.filter((id) => id !== device.id))
                        }}
                      />
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
                    {!device.hasOpenClaw ? <Tag color="warning">未安装 OpenClaw</Tag> : null}
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

      <Modal
        title="批量任务进度"
        open={batchModalOpen}
        onCancel={() => setBatchModalOpen(false)}
        footer={<Button onClick={() => setBatchModalOpen(false)}>关闭</Button>}
        width={860}
      >
        {batchJob ? (
          <Space direction="vertical" size={12} style={{ width: '100%' }}>
            <Typography.Text>
              命令：<Typography.Text code>{batchJob.command}</Typography.Text>
            </Typography.Text>
            <Space>
              <Typography.Text>状态：</Typography.Text>
              {batchStatusTag(batchJob.status)}
              <Typography.Text type="secondary">
                更新于 {formatDateTime(batchJob.updatedAt)}
              </Typography.Text>
            </Space>
            <Progress percent={batchProgress} />
            <Typography.Text type="secondary">
              成功 {batchJob.successCount} / 失败 {batchJob.errorCount} / 运行中 {batchJob.runningCount}
            </Typography.Text>
            <Table<BatchJobItem>
              rowKey="deviceId"
              columns={itemColumns}
              dataSource={batchJob.items}
              pagination={false}
              size="small"
            />
          </Space>
        ) : (
          <Skeleton active paragraph={{ rows: 6 }} />
        )}
      </Modal>
    </Space>
  )
}
