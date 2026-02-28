import {
  DesktopOutlined,
  DisconnectOutlined,
  LogoutOutlined,
  RobotOutlined,
} from '@ant-design/icons'
import { Avatar, Badge, Button, Layout, Menu, Space, Typography } from 'antd'
import { useMemo, type ReactNode } from 'react'
import { Link, useLocation, useNavigate } from 'react-router-dom'
import { useAuth } from '../contexts/AuthContext'
import { useRealtime } from '../contexts/RealtimeContext'

const { Header, Sider, Content } = Layout

export function AppLayout({ children }: { children: ReactNode }) {
  const location = useLocation()
  const navigate = useNavigate()
  const { logout } = useAuth()
  const { wsConnected } = useRealtime()

  const selectedMenu = useMemo(() => {
    if (location.pathname.startsWith('/devices/')) {
      return ['dashboard']
    }
    if (location.pathname.startsWith('/dashboard')) {
      return ['dashboard']
    }
    return []
  }, [location.pathname])

  return (
    <Layout className="app-shell">
      <Sider
        breakpoint="lg"
        collapsedWidth={60}
        className="app-sider"
        theme="dark"
        width={220}
      >
        <div className="app-logo" onClick={() => navigate('/dashboard')}>
          <RobotOutlined />
          <span>ClawMini</span>
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
          ]}
        />
      </Sider>
      <Layout>
        <Header className="app-header">
          <div className="app-header-title">
            <Typography.Title level={4} style={{ margin: 0 }}>
              设备管理平台
            </Typography.Title>
            <Typography.Text type="secondary">ClawMini Web 控制台</Typography.Text>
          </div>

          <Space size="middle">
            <Badge
              status={wsConnected ? 'processing' : 'default'}
              text={wsConnected ? '实时连接正常' : '实时连接断开'}
            />
            <Avatar icon={wsConnected ? <RobotOutlined /> : <DisconnectOutlined />} />
            <Button
              icon={<LogoutOutlined />}
              onClick={() => {
                logout()
                navigate('/login')
              }}
            >
              退出登录
            </Button>
          </Space>
        </Header>
        <Content className="app-content">{children}</Content>
      </Layout>
    </Layout>
  )
}
