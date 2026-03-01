import { LockOutlined, SafetyOutlined, UserOutlined } from '@ant-design/icons'
import { Alert, Button, Card, Form, Input, Space, Typography } from 'antd'
import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useAuth } from '../contexts/AuthContext'

interface LoginFormValues {
  username: string
  password: string
}

export function LoginPage() {
  const [form] = Form.useForm<LoginFormValues>()
  const [error, setError] = useState('')
  const navigate = useNavigate()
  const { loading, loginWithPassword } = useAuth()

  const onFinish = async (values: LoginFormValues) => {
    setError('')
    try {
      await loginWithPassword(values.username, values.password)
      navigate('/dashboard', { replace: true })
    } catch (err) {
      setError(err instanceof Error ? err.message : '登录失败，请检查用户名和密码')
    }
  }

  return (
    <div className="login-page">
      <Card className="login-card" bordered={false}>
        <Space direction="vertical" size={20} style={{ width: '100%' }}>
          <div>
            <Typography.Title level={2} style={{ marginBottom: 8 }}>
              ClawMini 控制台
            </Typography.Title>
            <Typography.Text type="secondary">使用用户名和密码登录</Typography.Text>
          </div>

          {error ? <Alert type="error" showIcon message={error} /> : null}

          <Form form={form} layout="vertical" onFinish={onFinish} autoComplete="off">
            <Form.Item label="用户名" name="username" rules={[{ required: true, message: '请输入用户名' }]}>
              <Input size="large" prefix={<UserOutlined />} placeholder="请输入用户名" />
            </Form.Item>

            <Form.Item label="密码" name="password" rules={[{ required: true, message: '请输入密码' }]}>
              <Input.Password size="large" prefix={<LockOutlined />} placeholder="请输入密码" />
            </Form.Item>

            <Button type="primary" htmlType="submit" block size="large" loading={loading}>
              登录
            </Button>
          </Form>

          <Space>
            <SafetyOutlined style={{ color: '#0f766e' }} />
            <Typography.Text type="secondary">登录态仅保存在当前会话（sessionStorage）</Typography.Text>
          </Space>
        </Space>
      </Card>
    </div>
  )
}
