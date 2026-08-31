import { useEffect, useState } from 'react'
import api from '../api'
import type { KbaseDir, KbaseFile, QASession, QAMessage } from '../types'

export default function Knowledge() {
  const [scope, setScope] = useState<'public' | 'private'>('private')
  const [dirID, setDirID] = useState(0)
  const [dirs, setDirs] = useState<KbaseDir[]>([])
  const [files, setFiles] = useState<KbaseFile[]>([])
  const [parentID, setParentID] = useState(0)
  const [newDirName, setNewDirName] = useState('')
  const [targetFileID, setTargetFileID] = useState<number>(0)

  // 问答
  const [sessions, setSessions] = useState<QASession[]>([])
  const [curSession, setCurSession] = useState<number>(0)
  const [messages, setMessages] = useState<QAMessage[]>([])
  const [question, setQuestion] = useState('')

  const loadKbase = async () => {
    const d = (await api.get(`/kbase/dir?scope=${scope}&dir_id=${dirID}&parent_id=${parentID}`)) as any
    setDirs(d?.dirs || [])
    setFiles(d?.files || [])
  }

  const loadSessions = async () => {
    const s = (await api.get('/qa/sessions')) as any
    setSessions(s || [])
  }

  useEffect(() => {
    loadKbase()
    loadSessions()
  }, [scope, dirID])

  const upload = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    if (!file) return
    const fd = new FormData()
    fd.append('scope', scope)
    fd.append('dir_id', String(dirID))
    fd.append('file', file)
    if (targetFileID) fd.append('target_file_id', String(targetFileID))
    await api.post('/kbase/file', fd)
    e.target.value = ''
    loadKbase()
  }

  const mkdir = async () => {
    if (!newDirName) return
    await api.post('/kbase/dir', { scope, parent_id: dirID, name: newDirName })
    setNewDirName('')
    loadKbase()
  }

  const removeFile = async (id: number) => {
    if (!confirm('确认删除文件？')) return
    await api.delete(`/kbase/file/${id}?scope=${scope}`)
    loadKbase()
  }

  const removeDir = async (id: number) => {
    if (!confirm('确认删除目录？')) return
    await api.delete(`/kbase/dir/${id}?scope=${scope}`)
    loadKbase()
  }

  const preview = async (id: number) => {
    const r = (await api.get(`/kbase/file/${id}/preview`)) as any
    window.open(r.url, '_blank')
  }
  const download = async (id: number) => {
    const r = (await api.get(`/kbase/file/${id}/download`)) as any
    window.open(r.url, '_blank')
  }

  // 问答
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
    if (!question || !curSession) return
    const r = (await api.post(`/qa/sessions/${curSession}/ask`, { question })) as any
    setMessages((m) => [...m, { id: Date.now(), role: 'user', content: question }, { id: Date.now() + 1, role: 'assistant', content: r.answer }])
    setQuestion('')
    loadSessions()
  }

  return (
    <div style={{ display: 'flex', gap: 16, height: '100%' }}>
      {/* 左：知识库管理 */}
      <div style={{ flex: 1, borderRight: '1px solid #eee' }}>
        <div>
          <button onClick={() => setScope('private')} disabled={scope === 'private'}>私有库</button>
          <button onClick={() => setScope('public')} disabled={scope === 'public'} style={{ marginLeft: 8 }}>公有库</button>
        </div>
        <div style={{ margin: '8px 0' }}>
          <input placeholder="新建目录名" value={newDirName} onChange={(e) => setNewDirName(e.target.value)} />
          <button onClick={mkdir} style={{ marginLeft: 8 }}>建目录</button>
          <label style={{ marginLeft: 16 }}>
            上传文件
            <input type="file" accept=".txt,.md,.markdown" onChange={upload} style={{ display: 'none' }} />
          </label>
          <input placeholder="覆盖文件ID(可选)" value={targetFileID || ''} onChange={(e) => setTargetFileID(Number(e.target.value))} style={{ width: 100, marginLeft: 8 }} />
        </div>

        <div>
          <strong>目录</strong>
          {dirs.map((d) => (
            <div key={d.id}>
              <a onClick={() => { setParentID(dirID); setDirID(d.id) }} style={{ cursor: 'pointer' }}>{d.name}</a>
              <button onClick={() => removeDir(d.id)} style={{ marginLeft: 8 }}>删</button>
            </div>
          ))}
          {dirID !== 0 && <div><a onClick={() => { setDirID(parentID); setParentID(0) }} style={{ cursor: 'pointer' }}>.. 返回上级</a></div>}
        </div>

        <div>
          <strong>文件</strong>
          {files.map((f) => (
            <div key={f.id}>
              {f.name} ({f.file_type})
              <button onClick={() => preview(f.id)} style={{ marginLeft: 8 }}>预览</button>
              <button onClick={() => download(f.id)}>下载</button>
              <button onClick={() => removeFile(f.id)}>删</button>
            </div>
          ))}
        </div>
      </div>

      {/* 右：知识库问答 */}
      <div style={{ flex: 1 }}>
        <div>
          <button onClick={newSession}>新建会话</button>
          <div style={{ maxHeight: 200, overflow: 'auto' }}>
            {sessions.map((s) => (
              <div key={s.id}><a onClick={() => openSession(s.id)} style={{ cursor: 'pointer' }}>{s.title}</a></div>
            ))}
          </div>
        </div>
        <div style={{ height: 300, overflow: 'auto', border: '1px solid #eee', padding: 8, margin: '8px 0' }}>
          {messages.map((m) => (
            <div key={m.id} style={{ textAlign: m.role === 'user' ? 'right' : 'left', margin: '4px 0' }}>
              <span style={{ background: m.role === 'user' ? '#e0f2fe' : '#f3f4f6', padding: '4px 8px', borderRadius: 6, display: 'inline-block' }}>{m.content}</span>
            </div>
          ))}
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          <input style={{ flex: 1 }} placeholder="提问" value={question} onChange={(e) => setQuestion(e.target.value)} />
          <button onClick={ask} disabled={!curSession}>发送</button>
        </div>
      </div>
    </div>
  )
}
