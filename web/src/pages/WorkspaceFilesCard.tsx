import { DeleteOutlined, EditOutlined, FileAddOutlined, ReloadOutlined } from '@ant-design/icons'
import {
  Alert,
  Button,
  Card,
  Empty,
  Form,
  Input,
  List,
  Modal,
  Space,
  Tag,
  Typography,
  message,
} from 'antd'
import { useCallback, useEffect, useState } from 'react'

const API_BASE = '/api'

async function apiRequest<T>(path: string, token: string | null, opts?: RequestInit): Promise<T> {
  const res = await fetch(API_BASE + path, {
    ...opts,
    headers: { ...(opts?.headers as Record<string, string> || {}), ...(token ? { Authorization: 'Bearer ' + token } : {}) },
  })
  if (!res.ok) throw new Error(await res.text())
  return res.json() as Promise<T>
}

const FILE_ICONS: Record<string, string> = {
  'SOUL.md': '❤️',
  'MEMORY.md': '🧠',
  'AGENTS.md': '🤖',
  'USER.md': '👤',
}

function getFileIcon(filename: string): string {
  const base = filename.split('/').pop() || filename
  return FILE_ICONS[base] || '📄'
}

interface Props {
  token: string | null
  deviceId: string
  online: boolean
}

export default function WorkspaceFilesCard({ token, deviceId, online }: Props) {
  const [files, setFiles] = useState<string[]>([])
  const [loading, setLoading] = useState(false)
  const [editOpen, setEditOpen] = useState(false)
  const [editingFile, setEditingFile] = useState<string | null>(null)
  const [editContent, setEditContent] = useState('')
  const [editLoading, setEditLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [newFileOpen, setNewFileOpen] = useState(false)
  const [newFileForm] = Form.useForm<{ filename: string; content: string }>()
  const [newFileSaving, setNewFileSaving] = useState(false)

  const fetchFiles = useCallback(async () => {
    if (!online) return
    setLoading(true)
    try {
      const data = await apiRequest<{ files?: string[]; error?: string }>(
        `/devices/${deviceId}/workspace-files`,
        token,
      )
      if (data && data.files) {
        setFiles(data.files.sort())
      } else {
        setFiles([])
      }
    } catch {
      // silent
    } finally {
      setLoading(false)
    }
  }, [token, deviceId, online])

  useEffect(() => {
    void fetchFiles()
  }, [fetchFiles])

  const openEditor = async (filename: string) => {
    setEditingFile(filename)
    setEditContent('')
    setEditOpen(true)
    setEditLoading(true)
    try {
      const data = await apiRequest<{ content?: string; error?: string }>(
        `/devices/${deviceId}/workspace-files/${encodeURIComponent(filename)}`,
        token,
      )
      if (data && typeof data.content === 'string') {
        setEditContent(data.content)
      }
    } catch {
      message.error('读取文件失败')
    } finally {
      setEditLoading(false)
    }
  }

  const handleSaveFile = async () => {
    if (!editingFile) return
    setSaving(true)
    try {
      await apiRequest(`/devices/${deviceId}/workspace-files/${encodeURIComponent(editingFile)}`, token, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ content: editContent }),
      })
      message.success('文件已保存')
      setEditOpen(false)
      triggerMemoryIndex()
    } catch {
      message.error('保存失败')
    } finally {
      setSaving(false)
    }
  }

  const handleDeleteFile = (filename: string) => {
    Modal.confirm({
      title: '删除文件',
      content: `确定删除 "${filename}" 吗？此操作不可撤销。`,
      okText: '删除',
      okType: 'danger',
      cancelText: '取消',
      onOk: async () => {
        try {
          await apiRequest(`/devices/${deviceId}/workspace-files/${encodeURIComponent(filename)}`, token, {
            method: 'DELETE',
          })
          message.success('已删除')
          void fetchFiles()
        } catch {
          message.error('删除失败')
        }
      },
    })
  }

  const handleNewFile = async () => {
    try {
      const values = await newFileForm.validateFields()
      setNewFileSaving(true)
      const filename = values.filename.endsWith('.md') ? values.filename : values.filename + '.md'
      await apiRequest(`/devices/${deviceId}/workspace-files/${encodeURIComponent(filename)}`, token, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ content: values.content || '' }),
      })
      message.success('文件已创建')
      setNewFileOpen(false)
      newFileForm.resetFields()
      void fetchFiles()
      triggerMemoryIndex()
    } catch (err: unknown) {
      if (err && typeof err === 'object' && 'errorFields' in err) return
      message.error('创建失败')
    } finally {
      setNewFileSaving(false)
    }
  }

  const triggerMemoryIndex = async () => {
    try {
      await apiRequest(`/devices/${deviceId}/exec`, token, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ command: 'openclaw', args: ['memory', 'index'], timeout: 120 }),
      })
    } catch {
      // silent - index will run on schedule anyway
    }
  }

  return (
    <Card
      title="Workspace 文件"
      extra={
        <Space>
          <Button icon={<ReloadOutlined />} onClick={() => void fetchFiles()} loading={loading} disabled={!online} size="small">
            刷新
          </Button>
          <Button type="primary" icon={<FileAddOutlined />} disabled={!online} size="small" onClick={() => { newFileForm.resetFields(); setNewFileOpen(true) }}>
            新建文件
          </Button>
        </Space>
      }
    >
      {!online && <Alert type="warning" showIcon message="设备离线，无法管理 Workspace 文件" style={{ marginBottom: 12 }} />}

      {files.length > 0 ? (
        <List
          size="small"
          dataSource={files}
          renderItem={(filename) => (
            <List.Item
              actions={[
                <Button key="edit" type="link" icon={<EditOutlined />} onClick={() => void openEditor(filename)} disabled={!online}>编辑</Button>,
                <Button key="delete" type="link" danger icon={<DeleteOutlined />} onClick={() => handleDeleteFile(filename)} disabled={!online}>删除</Button>,
              ]}
            >
              <Space>
                <span>{getFileIcon(filename)}</span>
                <Typography.Text
                  style={{ cursor: online ? 'pointer' : 'default' }}
                  onClick={online ? () => void openEditor(filename) : undefined}
                >
                  {filename}
                </Typography.Text>
                {filename.startsWith('memory/') && <Tag color="purple">memory</Tag>}
              </Space>
            </List.Item>
          )}
        />
      ) : (
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={loading ? '加载中...' : '暂无 .md 文件'} />
      )}

      {/* File Editor Modal */}
      <Modal
        title={editingFile ? `${getFileIcon(editingFile)} ${editingFile}` : '编辑文件'}
        open={editOpen}
        onCancel={() => setEditOpen(false)}
        onOk={() => void handleSaveFile()}
        confirmLoading={saving}
        okText="保存"
        cancelText="取消"
        width={800}
        destroyOnClose
      >
        {editLoading ? (
          <Typography.Text type="secondary">加载中...</Typography.Text>
        ) : (
          <Input.TextArea
            value={editContent}
            onChange={(e) => setEditContent(e.target.value)}
            rows={20}
            style={{ fontFamily: 'monospace', fontSize: 13 }}
            autoSize={{ minRows: 10, maxRows: 30 }}
          />
        )}
      </Modal>

      {/* New File Modal */}
      <Modal
        title="新建文件"
        open={newFileOpen}
        onCancel={() => setNewFileOpen(false)}
        onOk={() => void handleNewFile()}
        confirmLoading={newFileSaving}
        okText="创建"
        cancelText="取消"
        width={800}
        destroyOnClose
      >
        <Form form={newFileForm} layout="vertical" style={{ marginTop: 16 }}>
          <Form.Item
            name="filename"
            label="文件名"
            rules={[
              { required: true, message: '请输入文件名' },
              { pattern: /^[a-zA-Z0-9_\-/]+\.?[a-zA-Z0-9]*$/, message: '文件名只支持字母、数字、下划线、连字符和斜杠' },
            ]}
            extra="支持子目录如 memory/notes.md，自动添加 .md 扩展名"
          >
            <Input placeholder="SOUL.md 或 memory/notes.md" />
          </Form.Item>
          <Form.Item name="content" label="内容">
            <Input.TextArea
              rows={15}
              style={{ fontFamily: 'monospace', fontSize: 13 }}
              autoSize={{ minRows: 8, maxRows: 25 }}
              placeholder="# Title&#10;&#10;Write content here..."
            />
          </Form.Item>
        </Form>
      </Modal>
    </Card>
  )
}
