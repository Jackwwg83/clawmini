import { LockOutlined, SafetyOutlined } from '@ant-design/icons'
import { Alert, Button, Card, Form, Input, Space, Typography } from 'antd'
import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useAuth } from '../contexts/AuthContext'

interface LoginFormValues {
  token: string
}

export function LoginPage() {
  const [form] = Form.useForm<LoginFormValues>()
  const [error, setError] = useState('')
  const navigate = useNavigate()
  const { loading, loginWithToken } = useAuth()

  const onFinish = async (values: LoginFormValues) => {
    setError('')
    try {
      await loginWithToken(values.token)
      navigate('/dashboard', { replace: true })
    } catch (err) {
      setError(err instanceof Error ? err.message : '登录失败，请检查令牌')
    }
  }

  return (
    <div className="login-page">
      <Card className="login-card" bordered={false}>
        <Space direction="vertical" size={20} style={{ width: '100%' }}>
          <div>
            <Typography.Title level={2} style={{ marginBottom: 8 }}>
              ClawMini 管理后台
            </Typography.Title>
            <Typography.Text type="secondary">
              输入管理员令牌以访问设备控制台
            </Typography.Text>
          </div>

          {error ? <Alert type="error" showIcon message={error} /> : null}

          <Form form={form} layout="vertical" onFinish={onFinish} autoComplete="off">
            <Form.Item
              label="管理员令牌"
              name="token"
              rules={[{ required: true, message: '请输入管理员令牌' }]}
            >
              <Input.Password
                size="large"
                prefix={<LockOutlined />}
                placeholder="请输入管理员令牌"
              />
            </Form.Item>

            <Button type="primary" htmlType="submit" block size="large" loading={loading}>
              登录
            </Button>
          </Form>

          <Space>
            <SafetyOutlined style={{ color: '#0f766e' }} />
            <Typography.Text type="secondary">
              令牌仅保存在当前浏览器本地存储
            </Typography.Text>
          </Space>
        </Space>
      </Card>
    </div>
  )
}
