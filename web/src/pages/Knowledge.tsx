import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
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
  Modal,
  Tag,
  Tooltip,
  Spin,
  Dropdown,
} from 'antd'
import {
  FileTextOutlined,
  UploadOutlined,
  DeleteOutlined,
  EyeOutlined,
  DownloadOutlined,
  FolderOutlined,
  FolderOpenOutlined,
  SearchOutlined,
  ReloadOutlined,
  EditOutlined,
  MessageOutlined,
  FolderAddOutlined,
  MoreOutlined,
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

function fmtTime(t?: string): string {
  if (!t) return '—'
  const d = new Date(t)
  if (Number.isNaN(d.getTime())) return t
  return d.toLocaleString('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
}
function fmtSize(s?: number): string {
  if (!s) return ''
  return s >= 1024 ? `${(s / 1024).toFixed(1)} KB` : `${s} B`
}

export default function Knowledge() {
  const { message, modal } = App.useApp()
  const [scope, setScope] = useState<'private' | 'public'>('private')
  // 当前位置：从根目录到当前目录的完整目录链（末项即当前所在目录）
  const [path, setPath] = useState<{ id: number; name: string }[]>([{ id: 0, name: '根目录' }])
  const dirID = path[path.length - 1].id
  const [treeDirs, setTreeDirs] = useState<KbaseDir[]>([])
  const [curDirs, setCurDirs] = useState<KbaseDir[]>([])
  const [files, setFiles] = useState<KbaseFile[]>([])
  const [loadingFiles, setLoadingFiles] = useState(false)

  // 文件检索：仅文件名，当前目录内
  const [fileKeyword, setFileKeyword] = useState('')
  const [appliedFileQuery, setAppliedFileQuery] = useState('')

  // 新建目录 / 重命名（目录或文件）/ 预览 弹窗
  const [newDirModal, setNewDirModal] = useState(false)
  const [newDirName, setNewDirName] = useState('')
  const [renameModal, setRenameModal] = useState(false)
  const [renameTarget, setRenameTarget] = useState<{ type: 'dir' | 'file'; id: number; name: string } | null>(null)
  const [renameName, setRenameName] = useState('')
  const [previewModal, setPreviewModal] = useState(false)
  const [previewData, setPreviewData] = useState<{ name: string; content: string } | null>(null)
  const [previewLoading, setPreviewLoading] = useState(false)

  // 问答会话
  const [sessions, setSessions] = useState<QASession[]>([])
  const [curSession, setCurSession] = useState<number | null>(null)
  const [messages, setMessages] = useState<QAMessage[]>([])
  const [question, setQuestion] = useState('')
  const [asking, setAsking] = useState(false)
  // 是否处于「新建但尚未提问」的草稿新会话（此时不落库，首问才真正创建）
  const [newPending, setNewPending] = useState(false)
  // 草稿新会话的本地虚 ID（负数，不与真实会话 id 冲突）
  const DRAFT_SESSION_ID = -1
  // 对话区底部锚点：消息更新（含「思考中」占位）后自动滚动到底部
  const msgEndRef = useRef<HTMLDivElement>(null)

  // 会话改名
  const [sessionRenameModal, setSessionRenameModal] = useState(false)
  const [sessionRenameId, setSessionRenameId] = useState(0)
  const [sessionRenameTitle, setSessionRenameTitle] = useState('')

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
      if (appliedFileQuery) fs = fs.filter((f) => f.name.includes(appliedFileQuery))
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

  // 消息变化（用户提问 / 思考中占位 / 答案返回）时自动滚动到对话区底部
  useEffect(() => {
    msgEndRef.current?.scrollIntoView({ behavior: 'smooth', block: 'end' })
  }, [messages])

  const refreshBrowse = () => {
    loadTree()
    loadCurDir()
  }

  const goRoot = () => {
    setPath([{ id: 0, name: '根目录' }])
    setAppliedFileQuery('')
    setFileKeyword('')
  }
  // 文件区/列表点「子目录」进入：追加到当前位置链
  const enterDir = (d: KbaseDir) => {
    setPath((p) => [...p, { id: d.id, name: d.name }])
    setAppliedFileQuery('')
    setFileKeyword('')
  }
  // 目录树点击：重置为「根到该目录」的完整链（替换而非追加）
  const selectDirByID = (id: number) => {
    const byId = new Map<number, KbaseDir>()
    treeDirs.forEach((x) => byId.set(x.id, x))
    const d = byId.get(id)
    if (!d) return
    const chain: { id: number; name: string }[] = [{ id: 0, name: '根目录' }]
    const ancestors: KbaseDir[] = []
    let cur = byId.get(d.parent_id)
    while (cur) {
      ancestors.unshift(cur)
      cur = byId.get(cur.parent_id)
    }
    ancestors.forEach((x) => chain.push({ id: x.id, name: x.name }))
    chain.push({ id: d.id, name: d.name })
    setPath(chain)
    setAppliedFileQuery('')
    setFileKeyword('')
  }
  // 面包屑点击第 index 层（0=根目录），当前位置跳到该层
  const goBreadcrumb = (index: number) => {
    setPath((p) => p.slice(0, index + 1))
    setAppliedFileQuery('')
    setFileKeyword('')
  }

  const applyFileSearch = () => {
    setAppliedFileQuery(fileKeyword.trim())
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

  const openRename = (type: 'dir' | 'file', id: number, name: string) => {
    setRenameTarget({ type, id, name })
    setRenameName(name)
    setRenameModal(true)
  }
  const doRename = async () => {
    if (!renameTarget || !renameName.trim()) return
    try {
      if (renameTarget.type === 'dir') {
        await api.put(`/kbase/dir/${renameTarget.id}?scope=${scope}`, { name: renameName.trim() })
      } else {
        await api.put(`/kbase/file/${renameTarget.id}?scope=${scope}`, { name: renameName.trim() })
      }
      message.success('已重命名')
      setRenameModal(false)
      refreshBrowse()
    } catch (e: any) {
      message.error(e.message || '重命名失败')
    }
  }

  const doDeleteDir = (d: KbaseDir) => {
    modal.confirm({
      title: '删除目录',
      content: `确定删除目录「${d.name}」？删除后其中的文件将不可恢复。`,
      okText: '删除',
      okButtonProps: { danger: true },
      cancelText: '取消',
      onOk: async () => {
        await api.delete(`/kbase/dir/${d.id}?scope=${scope}`)
        if (dirID === d.id) goRoot()
        message.success('已删除目录')
        refreshBrowse()
      },
    })
  }
  const doDeleteFile = (f: KbaseFile) => {
    modal.confirm({
      title: '删除文件',
      content: `确定删除文件「${f.name}」？删除后不可恢复，敏感操作确认继续？`,
      okText: '删除',
      okButtonProps: { danger: true },
      cancelText: '取消',
      onOk: async () => {
        await api.delete(`/kbase/file/${f.id}?scope=${scope}`)
        message.success('已删除文件')
        refreshBrowse()
      },
    })
  }

  const openPreview = async (f: KbaseFile) => {
    setPreviewModal(true)
    setPreviewLoading(true)
    setPreviewData({ name: f.name, content: '' })
    try {
      const r = (await api.get(`/kbase/file/${f.id}/content`)) as any
      setPreviewData({ name: r?.name || f.name, content: r?.content || '' })
    } catch (e: any) {
      setPreviewData({ name: f.name, content: `无法预览：${e.message || '读取失败'}` })
    } finally {
      setPreviewLoading(false)
    }
  }
  const downloadFile = async (f: KbaseFile) => {
    const r = (await api.get(`/kbase/file/${f.id}/download`)) as any
    window.open(r.url, '_blank')
  }

  // 会话操作
  const newSession = () => {
    // 惰性创建：新建会话此时不落库，仅进入「草稿新会话」态；
    // 用户至少提问一次后才会真正在服务端创建并保存，未提问的空会话不残留。
    setCurSession(DRAFT_SESSION_ID)
    setNewPending(true)
    setMessages([])
    setAsking(false)
  }
  const openSession = async (id: number) => {
    // 切到某一已有会话：取消草稿新会话态（其从未落库，直接丢弃）
    setNewPending(false)
    setCurSession(id)
    setAsking(false)
    const msgs = (await api.get(`/qa/sessions/${id}/messages`)) as any
    setMessages(msgs || [])
  }
  const ask = async () => {
    if (!question.trim() || !curSession || asking) return
    const q = question.trim()

    // 若当前是「草稿新会话」：先真正创建会话（拿到真实 id），再提问。
    // 这保证只有用户至少提问一次的会话才会被保存。
    let sessionID = curSession
    if (newPending && curSession === DRAFT_SESSION_ID) {
      try {
        const s = (await api.post('/qa/sessions')) as any
        sessionID = s.id
        setCurSession(sessionID)
        setNewPending(false)
      } catch (e: any) {
        message.error(e.message || '创建会话失败')
        return // 会话未能创建，放弃本次提问
      }
    }

    // 立即把用户问题和「思考中」占位移入对话区；不用发按钮 loading，便于看到完整时序
    const thinkingId = -Date.now() // 占位消息使用负数 id，避免与真实消息 id 冲突
    setMessages((m) => [
      ...m,
      { id: Date.now(), role: 'user', content: q },
      { id: thinkingId, role: 'assistant', content: '思考中…', pending: true },
    ])
    setQuestion('')
    setAsking(true)
    try {
      const r = (await api.post(`/qa/sessions/${sessionID}/ask`, { question: q })) as any
      // 答案返回：移除「思考中」占位，写入真实回答
      setMessages((m) => m.filter((x) => x.id !== thinkingId).concat({ id: Date.now(), role: 'assistant', content: r.answer }))
      loadSessions()
    } catch (e: any) {
      // 失败：移除占位，回填错误，并保留用户问题
      setMessages((m) => m.map((x) => (x.id === thinkingId ? { ...x, content: `提问失败：${e.message || '请重试'}`, pending: false } : x)))
      message.error(e.message || '提问失败')
    } finally {
      setAsking(false)
    }
  }
  const openSessionRename = (s: QASession) => {
    setSessionRenameId(s.id)
    setSessionRenameTitle(s.title || '')
    setSessionRenameModal(true)
  }
  const doSessionRename = async () => {
    try {
      await api.put(`/qa/sessions/${sessionRenameId}`, { title: sessionRenameTitle.trim() })
      message.success('已改名')
      setSessionRenameModal(false)
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

  const treeNodes = useMemo(() => buildTree(treeDirs), [treeDirs])
  // 会话列表：草稿新会话置顶显示（未提问不落库，仅前端存在）
  const sessionsWithDraft = useMemo(() => {
    if (!newPending) return sessions
    return [{ id: DRAFT_SESSION_ID, title: '新会话', temp: true }, ...sessions] as QASession[]
  }, [newPending, sessions, DRAFT_SESSION_ID])
  const breadcrumbItems = [
    ...path.map((s, i) => {
      const isLast = i === path.length - 1
      return {
        title: (
          <span
            style={{ cursor: 'pointer', fontWeight: isLast ? 600 : 'normal', color: isLast ? 'var(--accent)' : undefined }}
            onClick={() => goBreadcrumb(i)}
          >
            {i === 0 ? `当前位置：根目录` : s.name}
          </span>
        ),
      }
    }),
  ]

  const scopeHint =
    scope === 'private'
      ? '私有库内部是当前用户个人上传的文件，仅用户个人可见'
      : '公有库内部是企业/单位管理员账户上传的文件，企业/单位内部所有用户均可见'

  return (
    <div style={{ display: 'flex', gap: 16, height: '100%' }}>
      {/* 左：网盘浏览 */}
      <div className="app-card" style={{ flex: 2, padding: 16, height: 600, overflow: 'auto' }}>
        {/* 顶部：仅私有/公有切换 + 跟随主题说明 */}
        <div style={{ marginBottom: 4 }}>
          <Segmented
            value={scope}
            onChange={(v) => {
              setScope(v as 'private' | 'public')
              goRoot()
            }}
            options={[
              { label: '私有库', value: 'private' },
              { label: '公有库', value: 'public' },
            ]}
          />
        </div>
        <Typography.Text type="secondary" style={{ display: 'block', fontSize: 13, marginBottom: 16, color: 'var(--text-soft)' }}>
          {scopeHint}
        </Typography.Text>

        {/* 面包屑 + 右上角操作（上传/新建目录） */}
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', flexWrap: 'wrap', gap: 12, marginBottom: 12 }}>
          <Breadcrumb items={breadcrumbItems} />
          <Space>
            <Upload
              accept=".txt,.md,.markdown"
              showUploadList={false}
              beforeUpload={(file) => {
                uploadFile(file)
                return false
              }}
              multiple
            >
              <Button type="primary" icon={<UploadOutlined />}>
                上传到当前目录
              </Button>
            </Upload>
            <Button icon={<FolderAddOutlined />} onClick={() => { setNewDirName(''); setNewDirModal(true) }}>
              新建目录
            </Button>
            <Tooltip title="刷新">
              <Button icon={<ReloadOutlined />} onClick={refreshBrowse} />
            </Tooltip>
          </Space>
        </div>

        <div style={{ display: 'flex', gap: 16 }}>
          {/* 目录树 */}
          <div style={{ width: 240, borderRight: '1px solid var(--panel-border)', paddingRight: 12, minHeight: 400 }}>
            <Typography.Text strong>目录</Typography.Text>
            {treeNodes.length === 0 ? (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无目录" />
            ) : (
              <Tree
                showLine
                showIcon
                icon={(props) => (props.expanded ? <FolderOpenOutlined style={{ color: '#f6b33c' }} /> : <FolderOutlined style={{ color: '#f6b33c' }} />)}
                defaultExpandAll={false}
                treeData={treeNodes}
                selectedKeys={dirID ? [dirID] : []}
                onSelect={(_keys, info) => selectDirByID(info.node.key as number)}
                onExpand={(_keys, info) => {
                  void info
                }}
              />
            )}
          </div>

          {/* 文件区 */}
          <div style={{ flex: 1, minWidth: 0 }}>
            {/* 文件名检索：仅当前目录 */}
            <Space.Compact style={{ width: '100%', marginBottom: 12 }}>
              <Input
                placeholder="请输入文件名"
                prefix={<SearchOutlined />}
                value={fileKeyword}
                onChange={(e) => setFileKeyword(e.target.value)}
                onPressEnter={applyFileSearch}
                allowClear
              />
              <Button type="primary" icon={<SearchOutlined />} onClick={applyFileSearch}>
                检索
              </Button>
              {(appliedFileQuery) && (
                <Button onClick={() => { setFileKeyword(''); setAppliedFileQuery('') }}>清除</Button>
              )}
            </Space.Compact>

            <Spin spinning={loadingFiles}>
              {curDirs.length === 0 && files.length === 0 ? (
                <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="当前目录为空，点右上角「上传到当前目录」或「新建目录」" />
              ) : (
                <>
                  {/* 表头 */}
                  <div
                    style={{
                      display: 'flex',
                      alignItems: 'center',
                      padding: '6px 12px',
                      borderBottom: '1px solid var(--panel-border)',
                      fontWeight: 600,
                      color: 'var(--text-soft)',
                      fontSize: 13,
                    }}
                  >
                    <span style={{ flex: '1 1 0', minWidth: 0 }}>文件名</span>
                    <span style={{ width: 150, flexShrink: 0 }}>更新时间</span>
                    <span style={{ width: 80, flexShrink: 0, textAlign: 'right' }}>操作</span>
                  </div>

                  {/* 子目录：操作下拉(重命名/删除)，更新时间=目录内变动时间(updated_at) */}
                  {curDirs.map((d) => (
                    <div
                      key={d.id}
                      style={{
                        display: 'flex',
                        alignItems: 'center',
                        padding: '9px 12px',
                        borderBottom: '1px solid var(--panel-border)',
                      }}
                    >
                      <span style={{ flex: '1 1 0', minWidth: 0, display: 'flex', alignItems: 'center', gap: 8 }}>
                        <FolderOpenOutlined style={{ color: '#f6b33c', flexShrink: 0 }} />
                        <Typography.Link onClick={() => enterDir(d)} style={{ fontSize: 14, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                          {d.name}
                        </Typography.Link>
                      </span>
                      <span style={{ width: 150, flexShrink: 0, fontSize: 12, color: 'var(--text-soft)' }}>
                        {fmtTime(d.updated_at)}
                      </span>
                      <span style={{ width: 80, flexShrink: 0, textAlign: 'right' }}>
                        <Dropdown
                          trigger={['click']}
                          menu={{
                            items: [
                              { key: 'rename', icon: <EditOutlined />, label: '重命名' },
                              { type: 'divider' },
                              { key: 'delete', icon: <DeleteOutlined />, label: '删除', danger: true },
                            ],
                            onClick: ({ key, domEvent }) => {
                              domEvent.stopPropagation()
                              if (key === 'rename') openRename('dir', d.id, d.name)
                              if (key === 'delete') doDeleteDir(d)
                            },
                          }}
                        >
                          <Button type="text" size="small" icon={<MoreOutlined />} />
                        </Dropdown>
                      </span>
                    </div>
                  ))}

                  {/* 文件：操作下拉(预览/下载/重命名/删除)，更新时间=上传时间(created_at) */}
                  {files.map((f) => (
                    <div
                      key={f.id}
                      style={{
                        display: 'flex',
                        alignItems: 'center',
                        padding: '9px 12px',
                        borderBottom: '1px solid var(--panel-border)',
                      }}
                    >
                      <span style={{ flex: '1 1 0', minWidth: 0, display: 'flex', alignItems: 'center', gap: 8 }}>
                        <FileTextOutlined style={{ color: 'var(--accent)', flexShrink: 0 }} />
                        <Typography.Text ellipsis style={{ flex: '1 1 0', minWidth: 0 }}>{f.name}</Typography.Text>
                        <Tag style={{ marginInlineEnd: 0, flexShrink: 0 }}>{f.file_type}</Tag>
                        <Typography.Text type="secondary" style={{ fontSize: 12, flexShrink: 0 }}>{fmtSize(f.size)}</Typography.Text>
                      </span>
                      <span style={{ width: 150, flexShrink: 0, fontSize: 12, color: 'var(--text-soft)' }}>
                        {fmtTime(f.created_at)}
                      </span>
                      <span style={{ width: 80, flexShrink: 0, textAlign: 'right' }}>
                        <Dropdown
                          trigger={['click']}
                          menu={{
                            items: [
                              { key: 'preview', icon: <EyeOutlined />, label: '预览' },
                              { key: 'download', icon: <DownloadOutlined />, label: '下载' },
                              { type: 'divider' },
                              { key: 'rename', icon: <EditOutlined />, label: '重命名' },
                              { type: 'divider' },
                              { key: 'delete', icon: <DeleteOutlined />, label: '删除', danger: true },
                            ],
                            onClick: ({ key, domEvent }) => {
                              domEvent.stopPropagation()
                              if (key === 'preview') openPreview(f)
                              if (key === 'download') downloadFile(f)
                              if (key === 'rename') openRename('file', f.id, f.name)
                              if (key === 'delete') doDeleteFile(f)
                            },
                          }}
                        >
                          <Button type="text" size="small" icon={<MoreOutlined />} />
                        </Dropdown>
                      </span>
                    </div>
                  ))}
                </>
              )}
            </Spin>
          </div>
        </div>
      </div>

      {/* 右：知识问答 + 会话侧边栏 */}
      <div
        className="app-card"
        style={{ flex: 1, minWidth: 320, padding: 16, display: 'flex', flexDirection: 'column', height: 600 }}
      >
        <div className="page-header" style={{ marginBottom: 12 }}>
          <Typography.Title level={5} style={{ margin: 0 }}>
            知识库问答
          </Typography.Title>
          <Button type="primary" icon={<MessageOutlined />} onClick={newSession}>
            新建会话
          </Button>
        </div>

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
          {sessionsWithDraft.length === 0 ? (
            <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无会话" style={{ margin: '8px 0' }} />
          ) : (
            sessionsWithDraft.map((s) => (
              <div
                key={s.id}
                onClick={() => (s.temp ? setCurSession(DRAFT_SESSION_ID) : openSession(s.id))}
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
                <Space size={4} style={{ flex: 1, minWidth: 0 }}>
                  {s.temp && <Tag color="blue" style={{ marginInlineEnd: 0, flexShrink: 0 }}>新</Tag>}
                  <Typography.Text ellipsis style={{ flex: 1 }}>
                    {s.title || `会话 #${s.id}`}
                  </Typography.Text>
                </Space>
                <Space size={2} onClick={(e) => e.stopPropagation()}>
                  {s.temp ? (
                    <Button
                      size="small"
                      type="text"
                      danger
                      icon={<DeleteOutlined />}
                      onClick={() => { setNewPending(false); setCurSession(null); setMessages([]) }}
                    />
                  ) : (
                    <>
                      <Button size="small" type="text" icon={<EditOutlined />} onClick={() => openSessionRename(s)} />
                      <Button size="small" type="text" danger icon={<DeleteOutlined />} onClick={() => doDeleteSession(s.id)} />
                    </>
                  )}
                </Space>
              </div>
            ))
          )}
        </div>

        <div
          style={{
            flex: 1,
            minHeight: 0, // 配合 flex 列布局 + overflow，实现固定高度内部滚动
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
                    display: 'inline-flex',
                    alignItems: 'center',
                    gap: 6,
                    maxWidth: '88%',
                    border: m.role === 'assistant' ? '1px solid var(--panel-border)' : 'none',
                    color: m.pending ? 'var(--text-soft)' : undefined,
                  }}
                >
                  {m.pending && <Spin size="small" />}
                  {m.content}
                </Typography.Text>
              </div>
            ))
          )}
          <div ref={msgEndRef} />
        </div>

        <Space.Compact style={{ width: '100%' }}>
          <Input
            placeholder="基于知识库提问"
            value={question}
            onChange={(e) => setQuestion(e.target.value)}
            onPressEnter={ask}
            disabled={!curSession}
          />
          <Button type="primary" onClick={ask} disabled={!curSession || asking}>
            发送
          </Button>
        </Space.Compact>      </div>

      {/* 新建目录弹窗 */}
      <Modal
        title={`在当前目录新建子目录`}
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

      {/* 重命名弹窗（目录/文件通用） */}
      <Modal
        title={renameTarget ? `重命名${renameTarget.type === 'dir' ? '目录' : '文件'}` : ''}
        open={renameModal}
        onOk={doRename}
        onCancel={() => setRenameModal(false)}
        okText="保存"
        cancelText="取消"
        width={380}
      >
        <Input
          placeholder={renameTarget?.type === 'dir' ? '新目录名' : '新文件名'}
          value={renameName}
          onChange={(e) => setRenameName(e.target.value)}
          onPressEnter={doRename}
          style={{ marginTop: 16 }}
        />
      </Modal>

      {/* 预览弹窗（内置文本预览） */}
      <Modal
        title={previewData?.name || '预览'}
        open={previewModal}
        onCancel={() => setPreviewModal(false)}
        footer={null}
        width={720}
        style={{ top: 40 }}
      >
        <pre
          style={{
            whiteSpace: 'pre-wrap',
            wordBreak: 'break-word',
            maxHeight: '60vh',
            overflow: 'auto',
            background: 'var(--panel-bg)',
            border: '1px solid var(--panel-border)',
            borderRadius: 8,
            padding: 14,
            margin: 0,
            fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace',
            fontSize: 13,
          }}
        >
          {previewLoading ? '加载中…' : previewData?.content || ''}
        </pre>
      </Modal>

      {/* 会话改名弹窗 */}
      <Modal
        title="改会话标题"
        open={sessionRenameModal}
        onOk={doSessionRename}
        onCancel={() => setSessionRenameModal(false)}
        okText="保存"
        cancelText="取消"
        width={380}
      >
        <Input
          placeholder="新标题"
          value={sessionRenameTitle}
          onChange={(e) => setSessionRenameTitle(e.target.value)}
          onPressEnter={doSessionRename}
          style={{ marginTop: 16 }}
        />
      </Modal>
    </div>
  )
}
