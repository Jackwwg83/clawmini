import { ReloadOutlined, SearchOutlined } from '@ant-design/icons'
import { Button, Card, DatePicker, Select, Skeleton, Space, Table, Tag, Typography, message } from 'antd'
import type { Dayjs } from 'dayjs'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { fetchAuditLog } from '../api/client'
import { useAuth } from '../contexts/AuthContext'
import { useRealtime } from '../contexts/RealtimeContext'
import type { AuditLogEntry } from '../types'
import { formatDateTime } from '../utils/format'

const { RangePicker } = DatePicker

const ACTION_OPTIONS = [
  { label: '全部操作', value: '' },
  { label: '命令执行', value: 'command.exec' },
  { label: '批量操作', value: 'batch.exec' },
  { label: '设备删除', value: 'device.delete' },
  { label: 'IM 配置', value: 'im.configure' },
  { label: '安装 OpenClaw', value: 'openclaw.install' },
]

function resultTag(result: string) {
  const normalized = result.trim().toLowerCase()
  if (normalized === 'success') {
    return <Tag color="success">成功</Tag>
  }
  if (normalized === 'accepted') {
    return <Tag color="processing">已受理</Tag>
  }
  if (normalized === 'rejected') {
    return <Tag color="warning">已拒绝</Tag>
  }
  if (normalized === 'failed') {
    return <Tag color="error">失败</Tag>
  }
  return <Tag>{result || '未知'}</Tag>
}

export function AuditLogPage() {
  const { token } = useAuth()
  const { devices } = useRealtime()
  const [loading, setLoading] = useState(false)
  const [initialLoading, setInitialLoading] = useState(true)
  const [items, setItems] = useState<AuditLogEntry[]>([])
  const [total, setTotal] = useState(0)
  const [offset, setOffset] = useState(0)
  const [limit, setLimit] = useState(20)

  const [selectedDeviceID, setSelectedDeviceID] = useState('')
  const [selectedAction, setSelectedAction] = useState('')
  const [selectedRange, setSelectedRange] = useState<[Dayjs, Dayjs] | null>(null)

  const loadAuditLog = useCallback(
    async (nextOffset: number, nextLimit: number) => {
      if (!token) {
        return
      }
      setLoading(true)
      try {
        const page = await fetchAuditLog(token, {
          limit: nextLimit,
          offset: nextOffset,
          deviceId: selectedDeviceID || undefined,
          action: selectedAction || undefined,
          from: selectedRange ? selectedRange[0].startOf('day').unix() : undefined,
          to: selectedRange ? selectedRange[1].endOf('day').unix() : undefined,
        })
        setItems(page.items)
        setTotal(page.total)
      } catch (err) {
        message.error(err instanceof Error ? err.message : '加载审计日志失败')
      } finally {
        setLoading(false)
        setInitialLoading(false)
      }
    },
    [selectedAction, selectedDeviceID, selectedRange, token],
  )

  useEffect(() => {
    void loadAuditLog(0, limit)
    setOffset(0)
  }, [limit, loadAuditLog, selectedAction, selectedDeviceID, selectedRange])

  const deviceOptions = useMemo(
    () => [
      { label: '全部设备', value: '' },
      ...devices.map((device) => ({
        label: `${device.hostname || device.id} (${device.id})`,
        value: device.id,
      })),
    ],
    [devices],
  )

  const columns = [
    {
      title: '时间',
      dataIndex: 'timestamp',
      key: 'timestamp',
      width: 190,
      render: (value: number) => formatDateTime(value),
    },
    {
      title: '操作',
      dataIndex: 'action',
      key: 'action',
      width: 150,
      render: (value: string) => value || '--',
    },
    {
      title: '设备',
      dataIndex: 'targetDeviceId',
      key: 'targetDeviceId',
      width: 220,
      render: (value?: string) => value || '--',
    },
    {
      title: '详情',
      dataIndex: 'detail',
      key: 'detail',
      ellipsis: true,
      render: (value?: string) => value || '--',
    },
    {
      title: '结果',
      dataIndex: 'result',
      key: 'result',
      width: 110,
      render: (value: string) => resultTag(value),
    },
  ]

  return (
    <Space direction="vertical" size={16} style={{ width: '100%' }}>
      <Card>
        <Space style={{ width: '100%', justifyContent: 'space-between' }} wrap>
          <Space direction="vertical" size={4}>
            <Typography.Title level={4} style={{ margin: 0 }}>
              审计日志
            </Typography.Title>
            <Typography.Text type="secondary">记录管理员关键操作，保留 90 天。</Typography.Text>
          </Space>
          <Button icon={<ReloadOutlined />} onClick={() => void loadAuditLog(offset, limit)} loading={loading}>
            刷新
          </Button>
        </Space>
      </Card>

      <Card>
        <Space wrap>
          <Select
            value={selectedDeviceID}
            options={deviceOptions}
            style={{ minWidth: 260 }}
            onChange={(value) => setSelectedDeviceID(value)}
            showSearch
            optionFilterProp="label"
          />
          <Select
            value={selectedAction}
            options={ACTION_OPTIONS}
            style={{ width: 180 }}
            onChange={(value) => setSelectedAction(value)}
          />
          <RangePicker
            value={selectedRange}
            onChange={(value) => {
              if (!value || value.length !== 2 || !value[0] || !value[1]) {
                setSelectedRange(null)
                return
              }
              setSelectedRange([value[0], value[1]])
            }}
          />
          <Button
            icon={<SearchOutlined />}
            onClick={() => {
              setOffset(0)
              void loadAuditLog(0, limit)
            }}
          >
            查询
          </Button>
          <Button
            onClick={() => {
              setSelectedDeviceID('')
              setSelectedAction('')
              setSelectedRange(null)
              setOffset(0)
            }}
          >
            重置
          </Button>
        </Space>
      </Card>

      <Card>
        {initialLoading ? (
          <Skeleton active paragraph={{ rows: 8 }} />
        ) : (
          <Table<AuditLogEntry>
            rowKey="id"
            columns={columns}
            dataSource={items}
            loading={loading}
            scroll={{ x: 980 }}
            pagination={{
              total,
              current: Math.floor(offset / limit) + 1,
              pageSize: limit,
              showSizeChanger: true,
              showQuickJumper: true,
              onChange: (page, pageSize) => {
                const nextLimit = pageSize || limit
                const nextOffset = (page - 1) * nextLimit
                setLimit(nextLimit)
                setOffset(nextOffset)
                void loadAuditLog(nextOffset, nextLimit)
              },
            }}
          />
        )}
      </Card>
    </Space>
  )
}
