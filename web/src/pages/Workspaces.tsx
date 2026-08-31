import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import api from '../api'
import type { Workspace } from '../types'

export default function Workspaces() {
  const navigate = useNavigate()
  const [list, setList] = useState<Workspace[]>([])
  const [title, setTitle] = useState('')
  const [newTitle, setNewTitle] = useState('')
  const [status, setStatus] = useState('')
  const [tag, setTag] = useState('')
  const [platform, setPlatform] = useState('')

  const load = async () => {
    const params = new URLSearchParams()
    if (title) params.set('title', title)
    if (status) params.set('status', status)
    if (tag) params.set('tag', tag)
    if (platform) params.set('platform', platform)
    const data = (await api.get(`/workspaces?${params}`)) as any
    setList(data || [])
  }

  useEffect(() => {
    load()
  }, [])

  const create = async () => {
    if (!newTitle) return
    await api.post('/workspaces', { title: newTitle })
    setNewTitle('')
    load()
  }

  const remove = async (id: number) => {
    if (!confirm('确认删除该工作区？')) return
    await api.delete(`/workspaces/${id}`)
    load()
  }

  return (
    <div>
      <div style={{ display: 'flex', gap: 8, marginBottom: 16, alignItems: 'center' }}>
        <input placeholder="新建工作区标题" value={newTitle} onChange={(e) => setNewTitle(e.target.value)} />
        <button onClick={create}>新建</button>
      </div>
      <div style={{ display: 'flex', gap: 8, marginBottom: 16 }}>
        <input placeholder="标题" value={title} onChange={(e) => setTitle(e.target.value)} />
        <input placeholder="标签" value={tag} onChange={(e) => setTag(e.target.value)} />
        <input placeholder="平台" value={platform} onChange={(e) => setPlatform(e.target.value)} />
        <input placeholder="状态" value={status} onChange={(e) => setStatus(e.target.value)} />
        <button onClick={load}>检索</button>
      </div>

      <table style={{ width: '100%', borderCollapse: 'collapse' }}>
        <thead>
          <tr>
            <th style={th}>标题</th>
            <th style={th}>状态</th>
            <th style={th}>操作</th>
          </tr>
        </thead>
        <tbody>
          {list.map((w) => (
            <tr key={w.id}>
              <td style={td}><a onClick={() => navigate(`/workspaces/${w.id}`)} style={{ cursor: 'pointer' }}>{w.title}</a></td>
              <td style={td}>{w.status}</td>
              <td style={td}><button onClick={() => remove(w.id)}>删除</button></td>
            </tr>
          ))}
        </tbody>
      </table>
      {list.length === 0 && <p>暂无工作区</p>}
    </div>
  )
}

const th = { border: '1px solid #ddd', padding: 8, textAlign: 'left' as const }
const td = { border: '1px solid #ddd', padding: 8 }
