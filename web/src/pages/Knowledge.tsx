import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Button,
  Input,
  Space,
  App,
  Typography,
  Upload,
  Empty,
  Tree,
  Segmented,
  Breadcrumb,
  Select,
  Modal,
  Tag,
  Tooltip,
  Spin,
} from 'antd'
import {
  FileTextOutlined,
  UploadOutlined,
  DeleteOutlined,
  EyeOutlined,
  DownloadOutlined,
  FolderOpenOutlined,
  PlusOutlined,
  SearchOutlined,
  ReloadOutlined,
  EditOutlined,
  MessageOutlined,
} from '@ant-design/icons'
import api from '../api'
import type { KbaseDir, KbaseFile, QASession, QAMessage } from '../types'

// 把扁平目录列表组装成 antd Tree 的节点
type TreeNode = { title: string; key: number; children: TreeNode[] }
function buildTree(dirs: KbaseDir[]): TreeNode[] {
  const nodes: TreeNode[] = dirs.map((d) => ({ title: d.name, key: d.id, children: [] }))
  const map = new Map<number, TreeNode>()
  nodes.forEach((n) => map.set(n.key, n))
  const roots: TreeNode[] = []
  for (const d of dirs) {
    const node = map.get(d.id)!
    if (d.parent_id && map.has(d.parent_id)) {
      map.get(d.parent_id)!.children.push(node)
    } else {
      roots.push(node)
    }
  }
  return roots
}

export default function Knowledge() {
  const { message } = App.useApp()
  const [scope, setScope] = useState<'private' | 'public'>('private')
  const [dirID, setDirID] = useState(0)
  const [dirStack, setDirStack] = useState<{ id: number; name: string }[]>([])
  const [treeDirs, setTreeDirs] = useState<KbaseDir[]>([])
  const [curDirs, setCurDirs] = useState<KbaseDir[]>([])
  const [files, setFiles] = useState<KbaseFile[]>([])
  const [loadingFiles, setLoadingFiles] = useState(false)

  // 文件检索
  const [fileField, setFileField] = useState<'name' | 'file_type'>('name')
  const [fileKeyword, setFileKeyword] = useState('')
  const [appliedFileQuery, setAppliedFileQuery] = useState<{ name?: string; file_type?: string }>({})

  // 新建目录
  const [newDirModal, setNewDirModal] = useState(false)
  const [newDirName, setNewDirName] = useState('')

  // 问答会话
  const [sessions, setSessions] = useState<QASession[]>([])
  const [curSession, setCurSession] = useState<number | null>(null)
  const [messages, setMessages] = useState<QAMessage[]>([])
  const [question, setQuestion] = useState('')
  const [asking, setAsking] = useState(false)

  // 会话改名
  const [renameModal, setRenameModal] = useState(false)
  const [renameId, setRenameId] = useState(0)
  const [renameTitle, setRenameTitle] = useState('')

  const loadTree = useCallback(async () => {
    const d = (await api.get(`/kbase/tree?scope=${scope}`)) as any
    setTreeDirs(d?.dirs || [])
  }, [scope])

  const loadCurDir = useCallback(async () => {
    setLoadingFiles(true)
    try {
      const d = (await api.get(`/kbase/dir?scope=${scope}&dir_id=${dirID}`)) as any
      setCurDirs(d?.dirs || [])
      let fs: KbaseFile[] = d?.files || []
      if (appliedFileQuery.name) fs = fs.filter((f) => f.name.includes(appliedFileQuery.name!))
      if (appliedFileQuery.file_type) fs = fs.filter((f) => f.file_type === appliedFileQuery.file_type)
      setFiles(fs)
    } finally {
      setLoadingFiles(false)
    }
  }, [scope, dirID, appliedFileQuery])

  const loadSessions = useCallback(async () => {
    const s = (await api.get('/qa/sessions')) as any
    setSessions(s || [])
  }, [])

  useEffect(() => {
    loadTree()
    loadCurDir()
  }, [loadTree, loadCurDir])

  useEffect(() => {
    loadSessions()
  }, [loadSessions])

  const refreshBrowse = () => {
    loadTree()
    loadCurDir()
  }

  // 进入顶层/根
  const goRoot = () => {
    setDirID(0)
    setDirStack([])
  }
  const goDir = (id: number, name: string) => {
    setDirStack((s) => [...s, { id: dirID, name }])
    setDirID(id)
  }
  // 面包屑跳回第 index 层（index 对应 dirStack 下标；-1 表示根）
  const goBreadcrumb = (index: number) => {
    if (index < 0) {
      goRoot()
    } else {
      const target = dirStack[index].id
      setDirID(target)
      setDirStack((s) => s.slice(0, index))
    }
  }

  const applyFileSearch = () => {
    if (!fileKeyword.trim()) {
      setAppliedFileQuery({})
    } else if (fileField === 'file_type') {
      setAppliedFileQuery({ file_type: fileKeyword.trim() })
    } else {
      setAppliedFileQuery({ name: fileKeyword.trim() })
    }
  }

  const mkdir = async () => {
    if (!newDirName.trim()) return
    try {
      await api.post('/kbase/dir', { scope, parent_id: dirID, name: newDirName.trim() })
      message.success('目录已创建')
      setNewDirName('')
      setNewDirModal(false)
      refreshBrowse()
    } catch (e: any) {
      message.error(e.message || '创建失败')
    }
  }

  const uploadFile = async (file: File) => {
    const fd = new FormData()
    fd.append('scope', scope)
    fd.append('dir_id', String(dirID))
    fd.append('file', file)
    try {
      await api.post('/kbase/file', fd)
      message.success(`已上传: ${file.name}（异步解析中）`)
      refreshBrowse()
    } catch (e: any) {
      message.error(e.message || '上传失败')
    }
  }

  const openUrl = async (path: string) => {
    const r = (await api.get(path)) as any
    window.open(r.url, '_blank')
  }

  // 会话操作
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
  const openRename = (s: QASession) => {
    setRenameId(s.id)
    setRenameTitle(s.title || '')
    setRenameModal(true)
  }
  const doRename = async () => {
    try {
      await api.put(`/qa/sessions/${renameId}`, { title: renameTitle.trim() })
      message.success('已改名')
      setRenameModal(false)
      loadSessions()
    } catch (e: any) {
      message.error(e.message || '改名失败')
    }
  }
  const doDeleteSession = async (id: number) => {
    try {
      await api.delete(`/qa/sessions/${id}`)
      message.success('已删除会话')
      if (curSession === id) {
        setCurSession(null)
        setMessages([])
      }
      loadSessions()
    } catch (e: any) {
      message.error(e.message || '删除失败')
    }
  }

  // 目录树节点
  const treeNodes = useMemo(() => buildTree(treeDirs), [treeDirs])
  // 当前选中的目录 key（面包屑顶部/当前）
  const breadcrumbItems = [
    {
      title: (
        <span style={{ cursor: 'pointer' }} onClick={goRoot}>
          <FolderOpenOutlined /> 根目录
        </span>
      ),
    },
    ...dirStack.map((s, i) => ({
      title: (
        <span style={{ cursor: 'pointer' }} onClick={() => goBreadcrumb(i)}>
          {s.name}
        </span>
      ),
    })),
  ]

  return (
    <div style={{ display: 'flex', gap: 16, height: '100%' }}>
      {/* 左：网盘浏览 */}
      <div className="app-card" style={{ flex: 2, padding: 16, minHeight: 520, overflow: 'auto' }}>
        {/* 顶部工具行 */}
        <Space wrap style={{ width: '100%', justifyContent: 'space-between', marginBottom: 16 }}>
          <Space wrap>
            <Segmented
              value={scope}
              onChange={(v) => {
                setScope(v as 'private' | 'public')
                goRoot()
                setAppliedFileQuery({})
              }}
              options={[
                { label: '私有库', value: 'private' },
                { label: '公有库', value: 'public' },
              ]}
            />
            <Upload
              accept=".txt,.md,.markdown"
              showUploadList={false}
              beforeUpload={(file) => {
                uploadFile(file)
                return false
              }}
              multiple
            >
              <Button icon={<UploadOutlined />}>上传到当前目录</Button>
            </Upload>
            <Button icon={<PlusOutlined />} onClick={() => { setNewDirName(''); setNewDirModal(true) }}>
              新建目录
            </Button>
          </Space>
          <Space>
            <Select
              value={fileField}
              onChange={setFileField}
              style={{ width: 92 }}
              options={[
                { label: '文件名', value: 'name' },
                { label: '文件类型', value: 'file_type' },
              ]}
            />
            <Input
              placeholder="搜索文件"
              style={{ width: 200 }}
              value={fileKeyword}
              onChange={(e) => setFileKeyword(e.target.value)}
              onPressEnter={applyFileSearch}
              allowClear
            />
            <Button type="primary" icon={<SearchOutlined />} onClick={applyFileSearch}>
              检索
            </Button>
            <Tooltip title="刷新">
              <Button icon={<ReloadOutlined />} onClick={refreshBrowse} />
            </Tooltip>
          </Space>
        </Space>

        <Breadcrumb items={breadcrumbItems} style={{ marginBottom: 12 }} />

        <div style={{ display: 'flex', gap: 16 }}>
          {/* 目录树 */}
          <div style={{ width: 250, borderRight: '1px solid var(--panel-border)', paddingRight: 12, minHeight: 400 }}>
            <Typography.Text strong>目录</Typography.Text>
            {treeNodes.length === 0 ? (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无目录" />
            ) : (
              <Tree
                showLine
                showIcon
                defaultExpandAll={false}
                treeData={treeNodes}
                selectedKeys={dirStack.length ? [dirStack[dirStack.length - 1].id] : dirID ? [dirID] : []}
                onSelect={(_keys, info) => {
                  const key = info.node.key as number
                  const d = treeDirs.find((x) => x.id === key)
                  if (d) goDir(d.id, d.name)
                }}
                onExpand={(_keys, info) => {
                  const key = info.node.key as number
                  if (info.expanded) {
                    const d = treeDirs.find((x) => x.id === key)
                    if (d) goDir(d.id, d.name)
                  }
                }}
              />
            )}
          </div>

          {/* 文件区 */}
          <Spin spinning={loadingFiles} style={{ flex: 1 }}>
            {/* 子目录 */}
            {curDirs.length > 0 && (
              <div style={{ marginBottom: 16 }}>
                {curDirs.map((d) => (
                  <div
                    key={d.id}
                    style={{
                      display: 'flex',
                      justifyContent: 'space-between',
                      alignItems: 'center',
                      padding: '6px 8px',
                      borderRadius: 6,
                    }}
                  >
                    <Typography.Link onClick={() => goDir(d.id, d.name)} style={{ fontSize: 14 }}>
                      <FolderOpenOutlined /> {d.name}
                    </Typography.Link>
                    <Button
                      danger
                      size="small"
                      icon={<DeleteOutlined />}
                      onClick={async (e) => {
                        e.preventDefault()
                        await api.delete(`/kbase/dir/${d.id}?scope=${scope}`)
                        refreshBrowse()
                      }}
                    />
                  </div>
                ))}
              </div>
            )}

            {/* 文件 */}
            {files.length === 0 && curDirs.length === 0 ? (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="当前目录为空，点上方「上传」或「新建目录」" />
            ) : files.length === 0 ? (
              <Typography.Text type="secondary">当前目录没有文件</Typography.Text>
            ) : (
              files.map((f) => (
                <div
                  key={f.id}
                  style={{
                    display: 'flex',
                    justifyContent: 'space-between',
                    alignItems: 'center',
                    padding: '8px 10px',
                    borderBottom: '1px solid var(--panel-border)',
                  }}
                >
                  <Space>
                    <FileTextOutlined style={{ color: 'var(--accent)' }} />
                    <Typography.Text>{f.name}</Typography.Text>
                    <Tag>{f.file_type}</Tag>
                    <Typography.Text type="secondary">{f.size >= 1024 ? `${(f.size / 1024).toFixed(1)} KB` : `${f.size} B`}</Typography.Text>
                  </Space>
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
                        refreshBrowse()
                      }}
                    >
                      删除
                    </Button>
                  </Space>
                </div>
              ))
            )}
          </Spin>
        </div>
      </div>

      {/* 右：知识问答 + 会话侧边栏 */}
      <div
        className="app-card"
        style={{ flex: 1, minWidth: 320, padding: 16, display: 'flex', flexDirection: 'column', minHeight: 520 }}
      >
        <div className="page-header" style={{ marginBottom: 12 }}>
          <Typography.Title level={5} style={{ margin: 0 }}>
            知识库问答
          </Typography.Title>
          <Button type="primary" icon={<MessageOutlined />} onClick={newSession}>
            新建会话
          </Button>
        </div>

        {/* 会话侧边栏 */}
        <div
          style={{
            border: '1px solid var(--panel-border)',
            borderRadius: 8,
            marginBottom: 12,
            maxHeight: 200,
            overflow: 'auto',
            padding: 4,
          }}
        >
          {sessions.length === 0 ? (
            <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无会话" style={{ margin: '8px 0' }} />
          ) : (
            sessions.map((s) => (
              <div
                key={s.id}
                onClick={() => openSession(s.id)}
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'space-between',
                  padding: '6px 8px',
                  cursor: 'pointer',
                  background: curSession === s.id ? 'rgba(79,110,245,0.12)' : 'transparent',
                  borderRadius: 6,
                }}
              >
                <Typography.Text ellipsis style={{ flex: 1 }}>
                  {s.title || `会话 #${s.id}`}
                </Typography.Text>
                <Space size={2} onClick={(e) => e.stopPropagation()}>
                  <Button size="small" type="text" icon={<EditOutlined />} onClick={() => openRename(s)} />
                  <Button size="small" type="text" danger icon={<DeleteOutlined />} onClick={() => doDeleteSession(s.id)} />
                </Space>
              </div>
            ))
          )}
        </div>

        {/* 对话窗口 */}
        <div
          style={{
            flex: 1,
            minHeight: 260,
            overflow: 'auto',
            border: '1px solid var(--panel-border)',
            borderRadius: 8,
            padding: 12,
            background: 'var(--panel-bg)',
            marginBottom: 12,
          }}
        >
          {messages.length === 0 ? (
            <Empty
              image={Empty.PRESENTED_IMAGE_SIMPLE}
              description={curSession ? '开始提问' : '请先新建或选择一个会话'}
            />
          ) : (
            messages.map((m) => (
              <div key={m.id} style={{ textAlign: m.role === 'user' ? 'right' : 'left', margin: '6px 0' }}>
                <Typography.Text
                  style={{
                    background: m.role === 'user' ? 'rgba(79,110,245,0.14)' : 'var(--panel-bg)',
                    padding: '6px 10px',
                    borderRadius: 8,
                    display: 'inline-block',
                    maxWidth: '88%',
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
            placeholder="基于知识库提问"
            value={question}
            onChange={(e) => setQuestion(e.target.value)}
            onPressEnter={ask}
            disabled={!curSession}
          />
          <Button type="primary" loading={asking} onClick={ask} disabled={!curSession}>
            发送
          </Button>
        </Space.Compact>
      </div>

      {/* 新建目录弹窗 */}
      <Modal
        title="新建目录"
        open={newDirModal}
        onOk={mkdir}
        onCancel={() => setNewDirModal(false)}
        okText="创建"
        cancelText="取消"
        width={380}
      >
        <Input
          placeholder="目录名称"
          value={newDirName}
          onChange={(e) => setNewDirName(e.target.value)}
          onPressEnter={mkdir}
          style={{ marginTop: 16 }}
        />
      </Modal>

      {/* 会话改名弹窗 */}
      <Modal
        title="改会话标题"
        open={renameModal}
        onOk={doRename}
        onCancel={() => setRenameModal(false)}
        okText="保存"
        cancelText="取消"
        width={380}
      >
        <Input
          placeholder="新标题"
          value={renameTitle}
          onChange={(e) => setRenameTitle(e.target.value)}
          onPressEnter={doRename}
          style={{ marginTop: 16 }}
        />
      </Modal>
    </div>
  )
}
