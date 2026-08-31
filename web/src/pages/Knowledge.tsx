import { useCallback, useEffect, useState } from 'react'
import {
  Card,
  Segmented,
  Button,
  Input,
  Space,
  List,
  App,
  Typography,
  Breadcrumb,
  Upload,
  Empty,
  Divider,
} from 'antd'
import {
  FolderOutlined,
  FileTextOutlined,
  PlusOutlined,
  UploadOutlined,
  DeleteOutlined,
  EyeOutlined,
  DownloadOutlined,
  MessageOutlined,
} from '@ant-design/icons'
import api from '../api'
import type { KbaseDir, KbaseFile, QASession, QAMessage } from '../types'

export default function Knowledge() {
  const { message } = App.useApp()
  const [scope, setScope] = useState<'private' | 'public'>('private')
  const [dirID, setDirID] = useState(0)
  const [dirStack, setDirStack] = useState<number[]>([])
  const [dirs, setDirs] = useState<KbaseDir[]>([])
  const [files, setFiles] = useState<KbaseFile[]>([])
  const [newDirName, setNewDirName] = useState('')
  const [targetFileID, setTargetFileID] = useState('')

  // 问答
  const [sessions, setSessions] = useState<QASession[]>([])
  const [curSession, setCurSession] = useState<number | null>(null)
  const [messages, setMessages] = useState<QAMessage[]>([])
  const [question, setQuestion] = useState('')
  const [asking, setAsking] = useState(false)

  const loadKbase = useCallback(async () => {
    const d = (await api.get(`/kbase/dir?scope=${scope}&dir_id=${dirID}`)) as any
    setDirs(d?.dirs || [])
    setFiles(d?.files || [])
  }, [scope, dirID])
  const loadSessions = useCallback(async () => {
    const s = (await api.get('/qa/sessions')) as any
    setSessions(s || [])
  }, [])

  useEffect(() => {
    loadKbase()
    loadSessions()
  }, [loadKbase, loadSessions])

  const mkdir = async () => {
    if (!newDirName.trim()) return
    try {
      await api.post('/kbase/dir', { scope, parent_id: dirID, name: newDirName.trim() })
      setNewDirName('')
      message.success('目录已创建')
      loadKbase()
    } catch (e: any) {
      message.error(e.message || '创建失败')
    }
  }

  const uploadFile = async (f: File) => {
    const fd = new FormData()
    fd.append('scope', scope)
    fd.append('dir_id', String(dirID))
    fd.append('file', f)
    if (targetFileID) fd.append('target_file_id', targetFileID)
    try {
      await api.post('/kbase/file', fd)
      message.success(`上传成功: ${f.name}（异步解析中）`)
      loadKbase()
    } catch (e: any) {
      message.error(e.message || '上传失败')
    }
  }

  const fileActions = (f: KbaseFile) => (
    <Space size="small">
      <Button size="small" icon={<EyeOutlined />} onClick={() => openUrl(`/kbase/file/${f.id}/preview`)}>
        预览
      </Button>
      <Button size="small" icon={<DownloadOutlined />} onClick={() => openUrl(`/kbase/file/${f.id}/download`)}>
        下载
      </Button>
      <Button
        danger
        size="small"
        icon={<DeleteOutlined />}
        onClick={async () => {
          await api.delete(`/kbase/file/${f.id}?scope=${scope}`)
          message.success('已删除')
          loadKbase()
        }}
      >
        删除
      </Button>
    </Space>
  )

  const openUrl = async (path: string) => {
    const r = (await api.get(path)) as any
    window.open(r.url, '_blank')
  }

  const newSession = async () => {
    const s = (await api.post('/qa/sessions')) as any
    setCurSession(s.id)
    setMessages([])
    loadSessions()
  }
  const openSession = async (id: number) => {
    setCurSession(id)
    const msgs = (await api.get(`/qa/sessions/${id}/messages`)) as any
    setMessages(msgs || [])
  }
  const ask = async () => {
    if (!question.trim() || !curSession) return
    setAsking(true)
    try {
      const r = (await api.post(`/qa/sessions/${curSession}/ask`, { question })) as any
      setMessages((m) => [
        ...m,
        { id: Date.now(), role: 'user', content: question },
        { id: Date.now() + 1, role: 'assistant', content: r.answer },
      ])
      setQuestion('')
      loadSessions()
    } catch (e: any) {
      message.error(e.message || '提问失败')
    } finally {
      setAsking(false)
    }
  }

  const breadcrumbItems = [
    { title: <span onClick={() => { setDirStack([]); setDirID(0) }} style={{ cursor: 'pointer' }}>根目录</span> },
    ...dirStack.map((id) => ({ title: `#${id}` })),
  ]

  return (
    <div style={{ display: 'flex', gap: 16, height: '100%' }}>
      {/* 左：知识库管理 */}
      <Card
        title="知识库"
        size="small"
        style={{ flex: 1, minHeight: 520, maxHeight: 'calc(100vh - 160px)', overflow: 'auto' }}
      >
        <Space direction="vertical" style={{ width: '100%' }} size={8}>
          <Segmented
            value={scope}
            onChange={(v) => { setScope(v as 'private' | 'public'); setDirID(0); setDirStack([]) }}
            options={[
              { label: '私有库', value: 'private' },
              { label: '公有库', value: 'public' },
            ]}
          />

          <Space wrap>
            <VerticalDivider />
            <Input
              placeholder="新建目录名"
              style={{ width: 140 }}
              value={newDirName}
              onChange={(e) => setNewDirName(e.target.value)}
              onPressEnter={mkdir}
            />
            <Button icon={<PlusOutlined />} onClick={mkdir}>
              建目录
            </Button>
            <div>&nbsp;</div>
            <Input
              placeholder="覆盖文件ID(可选)"
              style={{ width: 140 }}
              value={targetFileID}
              onChange={(e) => setTargetFileID(e.target.value)}
            />
            <Upload accept=".txt,.md,.markdown" showUploadList={false} beforeUpload={(f) => { uploadFile(f); return false }}>
              <Button icon={<UploadOutlined />}>上传文件</Button>
            </Upload>
          </Space>

          <Breadcrumb items={breadcrumbItems} />

          <div>
            <Typography.Text strong>目录</Typography.Text>
            {dirs.length === 0 ? (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无子目录" style={{ margin: '8px 0' }} />
            ) : (
              <List
                size="small"
                dataSource={dirs}
                locale={{ emptyText: ' ' }}
                renderItem={(d) => (
                  <List.Item
                    extra={[
                      <Button
                        danger
                        key="del"
                        size="small"
                        icon={<DeleteOutlined />}
                        onClick={async () => {
                          await api.delete(`/kbase/dir/${d.id}?scope=${scope}`)
                          loadKbase()
                        }}
                      />,
                    ]}
                  >
                    <Typography.Link onClick={() => { setDirStack((s) => [...s, dirID]); setDirID(d.id) }}>
                      <FolderOutlined /> {d.name}
                    </Typography.Link>
                  </List.Item>
                )}
              />
            )}
          </div>

          <Divider style={{ margin: '8px 0' }} />

          <div>
            <Typography.Text strong>文件</Typography.Text>
            {files.length === 0 ? (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无文件" style={{ margin: '8px 0' }} />
            ) : (
              <List
                size="small"
                dataSource={files}
                renderItem={(f) => (
                  <List.Item extra={fileActions(f)}>
                    <Space>
                      <FileTextOutlined />
                      <span>{f.name}</span>
                      <Typography.Text type="secondary">({f.file_type})</Typography.Text>
                    </Space>
                  </List.Item>
                )}
              />
            )}
          </div>
        </Space>
      </Card>

      {/* 右：知识库问答 */}
      <Card
        title="知识库问答"
        size="small"
        style={{ flex: 1, minHeight: 520, maxHeight: 'calc(100vh - 160px)', display: 'flex', flexDirection: 'column' }}
        extra={<Button icon={<MessageOutlined />} onClick={newSession}>新建会话</Button>}
      >
        <Space direction="vertical" style={{ width: '100%', flex: 1, display: 'flex' }}>
          <List
            size="small"
            style={{ maxHeight: 120, overflow: 'auto' }}
            dataSource={sessions}
            renderItem={(s) => (
              <List.Item onClick={() => openSession(s.id)} style={{ cursor: 'pointer' }}>
                <Typography.Text strong={curSession === s.id}>{s.title || `会话 #${s.id}`}</Typography.Text>
              </List.Item>
            )}
          />
          <div
            style={{
              flex: 1,
              minHeight: 240,
              overflow: 'auto',
              border: '1px solid var(--panel-border)',
              borderRadius: 8,
              padding: 12,
              background: 'var(--panel-bg)',
            }}
          >
            {messages.length === 0 ? (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="开始提问" />
            ) : (
              messages.map((m) => (
                <div key={m.id} style={{ textAlign: m.role === 'user' ? 'right' : 'left', margin: '6px 0' }}>
                  <Typography.Text
                    style={{
                      background: m.role === 'user' ? 'rgba(79,110,245,0.14)' : 'var(--panel-bg)',
                      padding: '6px 10px',
                      borderRadius: 8,
                      display: 'inline-block',
                      maxWidth: '85%',
                      border: m.role === 'assistant' ? '1px solid var(--panel-border)' : 'none',
                    }}
                  >
                    {m.content}
                  </Typography.Text>
                </div>
              ))
            )}
          </div>
          <Space.Compact style={{ width: '100%' }}>
            <Input
              placeholder="提问（基于知识库）"
              value={question}
              onChange={(e) => setQuestion(e.target.value)}
              onPressEnter={ask}
              disabled={!curSession}
            />
            <Button type="primary" loading={asking} onClick={ask} disabled={!curSession}>
              发送
            </Button>
          </Space.Compact>
        </Space>
      </Card>
    </div>
  )
}

function VerticalDivider() {
  return <span style={{ width: 0 }} />
}
