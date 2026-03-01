import {
  AuditOutlined,
  DesktopOutlined,
  DisconnectOutlined,
  LogoutOutlined,
  MessageOutlined,
  PlayCircleOutlined,
  RobotOutlined,
  TeamOutlined,
  UserOutlined,
} from '@ant-design/icons'
import { Avatar, Badge, Button, Dropdown, Form, Input, Layout, Menu, Modal, Space, Typography, message } from 'antd'
import { useMemo, useState, type ReactNode } from 'react'
import { Link, useLocation, useNavigate } from 'react-router-dom'
import { changeMyPassword } from '../api/client'
import { useAuth } from '../contexts/AuthContext'
import { useRealtime } from '../contexts/RealtimeContext'

const { Header, Sider, Content } = Layout

export function AppLayout({ children }: { children: ReactNode }) {
  const location = useLocation()
  const navigate = useNavigate()
  const { logout, user, token, isAdmin, refreshMe } = useAuth()
  const { wsConnected } = useRealtime()
  const [passwordModalOpen, setPasswordModalOpen] = useState(false)
  const [savingPassword, setSavingPassword] = useState(false)
  const [passwordForm] = Form.useForm<{ oldPassword: string; newPassword: string; confirmPassword: string }>()

  const selectedMenu = useMemo(() => {
    if (location.pathname.startsWith('/users/')) {
      return ['users']
    }
    if (location.pathname.startsWith('/users')) {
      return ['users']
    }
    if (location.pathname.startsWith('/im-config') || location.pathname.includes('/im-config')) {
      return ['im-config']
    }
    if (location.pathname.startsWith('/audit-log')) {
      return ['audit-log']
    }
    if (location.pathname.startsWith('/demo')) {
      return ['demo']
    }
    if (location.pathname.startsWith('/devices/')) {
      return ['dashboard']
    }
    if (location.pathname.startsWith('/dashboard')) {
      return ['dashboard']
    }
    return []
  }, [location.pathname])

  const onChangePassword = async () => {
    try {
      const values = await passwordForm.validateFields()
      if (!token) {
        return
      }
      setSavingPassword(true)
      await changeMyPassword(token, values.oldPassword, values.newPassword)
      message.success('密码修改成功，请重新登录')
      setPasswordModalOpen(false)
      passwordForm.resetFields()
      logout()
      navigate('/login', { replace: true })
    } catch (err) {
      if (err instanceof Error) {
        message.error(err.message)
      }
    } finally {
      setSavingPassword(false)
      void refreshMe()
    }
  }

  return (
    <Layout className="app-shell">
      <Sider breakpoint="lg" collapsedWidth={60} className="app-sider" theme="dark" width={220}>
        <div className="app-logo" onClick={() => navigate('/dashboard')}>
          <RobotOutlined />
          <div style={{ display: 'flex', flexDirection: 'column', lineHeight: 1.2 }}>
            <span style={{ fontSize: 16, fontWeight: 700 }}>ClawMini</span>
            <span style={{ fontSize: 10, opacity: 0.6, fontWeight: 400 }}>by 睿动AI</span>
          </div>
        </div>
        <Menu
          theme="dark"
          mode="inline"
          selectedKeys={selectedMenu}
          items={[
            {
              key: 'dashboard',
              icon: <DesktopOutlined />,
              label: <Link to="/dashboard">设备总览</Link>,
            },
            {
              key: 'im-config',
              icon: <MessageOutlined />,
              label: <Link to="/im-config">IM 配置</Link>,
            },
            {
              key: 'audit-log',
              icon: <AuditOutlined />,
              label: <Link to="/audit-log">审计日志</Link>,
            },
            {
              key: 'demo',
              icon: <PlayCircleOutlined />,
              label: <Link to="/demo">Demo Script</Link>,
            },
            ...(isAdmin
              ? [
                  {
                    key: 'users',
                    icon: <TeamOutlined />,
                    label: <Link to="/users">用户管理</Link>,
                  },
                ]
              : []),
          ]}
        />
      </Sider>
      <Layout>
        <Header className="app-header">
          <div className="app-header-title">
            <Typography.Title level={4} style={{ margin: 0 }}>
              设备管理平台
            </Typography.Title>
            <Typography.Text type="secondary">ClawMini 控制台 · 睿动AI</Typography.Text>
          </div>

          <Space size="middle">
            <Badge status={wsConnected ? 'processing' : 'default'} text={wsConnected ? '实时连接正常' : '实时连接断开'} />
            <Dropdown
              trigger={['click']}
              menu={{
                items: [
                  {
                    key: 'change-password',
                    icon: <UserOutlined />,
                    label: '修改密码',
                    onClick: () => setPasswordModalOpen(true),
                  },
                  {
                    type: 'divider',
                  },
                  {
                    key: 'logout',
                    icon: <LogoutOutlined />,
                    label: '退出登录',
                    onClick: () => {
                      logout()
                      navigate('/login')
                    },
                  },
                ],
              }}
            >
              <Button>
                <Space>
                  <Avatar size="small" icon={wsConnected ? <RobotOutlined /> : <DisconnectOutlined />} />
                  <span>{user?.displayName || user?.username || '未登录'}</span>
                  <Typography.Text type="secondary">{user?.role || ''}</Typography.Text>
                </Space>
              </Button>
            </Dropdown>
          </Space>
        </Header>
        <Content className="app-content">{children}</Content>
      </Layout>
      <Modal
        title="修改密码"
        open={passwordModalOpen}
        confirmLoading={savingPassword}
        onOk={() => void onChangePassword()}
        onCancel={() => {
          setPasswordModalOpen(false)
          passwordForm.resetFields()
        }}
      >
        <Form form={passwordForm} layout="vertical">
          <Form.Item label="当前密码" name="oldPassword" rules={[{ required: true, message: '请输入当前密码' }]}>
            <Input.Password />
          </Form.Item>
          <Form.Item label="新密码" name="newPassword" rules={[{ required: true, message: '请输入新密码' }, { min: 6, message: '密码至少6位' }]}>
            <Input.Password />
          </Form.Item>
          <Form.Item
            label="确认新密码"
            name="confirmPassword"
            dependencies={['newPassword']}
            rules={[
              { required: true, message: '请再次输入新密码' },
              ({ getFieldValue }) => ({
                validator(_, value) {
                  if (!value || getFieldValue('newPassword') === value) {
                    return Promise.resolve()
                  }
                  return Promise.reject(new Error('两次输入的密码不一致'))
                },
              }),
            ]}
          >
            <Input.Password />
          </Form.Item>
        </Form>
      </Modal>
    </Layout>
  )
}
