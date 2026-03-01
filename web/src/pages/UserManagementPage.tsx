import { DeleteOutlined, EditOutlined, PlusOutlined } from '@ant-design/icons'
import { Button, Card, Form, Input, Modal, Popconfirm, Select, Space, Table, Tag, message } from 'antd'
import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { createUser, deleteUser, fetchUsers, updateUser } from '../api/client'
import { useAuth } from '../contexts/AuthContext'
import type { UserSummary } from '../types'
import { formatDateTime } from '../utils/format'

interface CreateFormValues {
  username: string
  password: string
  role: 'admin' | 'user'
  displayName: string
}

interface EditFormValues {
  role: 'admin' | 'user'
  displayName: string
  password?: string
}

export function UserManagementPage() {
  const navigate = useNavigate()
  const { token } = useAuth()
  const [users, setUsers] = useState<UserSummary[]>([])
  const [loading, setLoading] = useState(false)
  const [createOpen, setCreateOpen] = useState(false)
  const [editOpen, setEditOpen] = useState(false)
  const [editingUser, setEditingUser] = useState<UserSummary | null>(null)
  const [createForm] = Form.useForm<CreateFormValues>()
  const [editForm] = Form.useForm<EditFormValues>()

  const loadUsers = async () => {
    if (!token) {
      return
    }
    setLoading(true)
    try {
      const list = await fetchUsers(token)
      setUsers(list)
    } catch (err) {
      message.error(err instanceof Error ? err.message : '加载用户失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void loadUsers()
  }, [token])

  const onCreate = async () => {
    try {
      const values = await createForm.validateFields()
      if (!token) {
        return
      }
      await createUser(token, values)
      message.success('用户创建成功')
      setCreateOpen(false)
      createForm.resetFields()
      await loadUsers()
    } catch (err) {
      if (err instanceof Error) {
        message.error(err.message)
      }
    }
  }

  const onEdit = async () => {
    if (!editingUser || !token) {
      return
    }
    try {
      const values = await editForm.validateFields()
      await updateUser(token, editingUser.id, {
        role: values.role,
        displayName: values.displayName,
        password: values.password?.trim() || undefined,
      })
      message.success('用户更新成功')
      setEditOpen(false)
      setEditingUser(null)
      editForm.resetFields()
      await loadUsers()
    } catch (err) {
      if (err instanceof Error) {
        message.error(err.message)
      }
    }
  }

  return (
    <Card
      title="用户管理"
      extra={
        <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreateOpen(true)}>
          新建用户
        </Button>
      }
    >
      <Table<UserSummary>
        rowKey="id"
        loading={loading}
        dataSource={users}
        onRow={(record) => ({
          onClick: () => navigate(`/users/${record.id}`),
        })}
        columns={[
          {
            title: '用户名',
            dataIndex: 'username',
            key: 'username',
          },
          {
            title: '显示名',
            dataIndex: 'displayName',
            key: 'displayName',
          },
          {
            title: '角色',
            dataIndex: 'role',
            key: 'role',
            render: (role: string) => <Tag color={role === 'admin' ? 'gold' : 'blue'}>{role}</Tag>,
          },
          {
            title: '设备数',
            dataIndex: 'deviceCount',
            key: 'deviceCount',
            width: 100,
          },
          {
            title: '创建时间',
            dataIndex: 'createdAt',
            key: 'createdAt',
            render: (value: number) => formatDateTime(value),
          },
          {
            title: '操作',
            key: 'actions',
            width: 150,
            render: (_, record) => (
              <Space
                onClick={(event) => {
                  event.stopPropagation()
                }}
              >
                <Button
                  type="link"
                  icon={<EditOutlined />}
                  onClick={() => {
                    setEditingUser(record)
                    editForm.setFieldsValue({
                      role: record.role as 'admin' | 'user',
                      displayName: record.displayName,
                      password: '',
                    })
                    setEditOpen(true)
                  }}
                >
                  编辑
                </Button>
                <Popconfirm
                  title="确定删除该用户？"
                  onConfirm={async () => {
                    if (!token) {
                      return
                    }
                    try {
                      await deleteUser(token, record.id)
                      message.success('用户已删除')
                      await loadUsers()
                    } catch (err) {
                      message.error(err instanceof Error ? err.message : '删除失败')
                    }
                  }}
                >
                  <Button danger type="link" icon={<DeleteOutlined />}>
                    删除
                  </Button>
                </Popconfirm>
              </Space>
            ),
          },
        ]}
      />

      <Modal
        title="新建用户"
        open={createOpen}
        onOk={() => void onCreate()}
        onCancel={() => {
          setCreateOpen(false)
          createForm.resetFields()
        }}
      >
        <Form form={createForm} layout="vertical" initialValues={{ role: 'user' }}>
          <Form.Item label="用户名" name="username" rules={[{ required: true, message: '请输入用户名' }]}>
            <Input />
          </Form.Item>
          <Form.Item label="显示名" name="displayName" rules={[{ required: true, message: '请输入显示名' }]}>
            <Input />
          </Form.Item>
          <Form.Item label="角色" name="role" rules={[{ required: true, message: '请选择角色' }]}>
            <Select
              options={[
                { value: 'user', label: '用户' },
                { value: 'admin', label: '管理员' },
              ]}
            />
          </Form.Item>
          <Form.Item label="密码" name="password" rules={[{ required: true, message: '请输入密码' }]}>
            <Input.Password />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title="编辑用户"
        open={editOpen}
        onOk={() => void onEdit()}
        onCancel={() => {
          setEditOpen(false)
          setEditingUser(null)
          editForm.resetFields()
        }}
      >
        <Form form={editForm} layout="vertical">
          <Form.Item label="显示名" name="displayName" rules={[{ required: true, message: '请输入显示名' }]}>
            <Input />
          </Form.Item>
          <Form.Item label="角色" name="role" rules={[{ required: true, message: '请选择角色' }]}>
            <Select
              options={[
                { value: 'user', label: '用户' },
                { value: 'admin', label: '管理员' },
              ]}
            />
          </Form.Item>
          <Form.Item label="重置密码（可选）" name="password">
            <Input.Password placeholder="留空则不修改" />
          </Form.Item>
        </Form>
      </Modal>
    </Card>
  )
}
