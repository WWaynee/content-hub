import { useCallback, useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Table, Input, Button, Space, App, Tag, Typography } from 'antd'
import { PlusOutlined, SearchOutlined, DeleteOutlined } from '@ant-design/icons'
import api from '../api'
import type { Workspace } from '../types'

const STATUS_COLOR: Record<string, string> = {
  draft: 'default',
  needs_req: 'warning',
  generating: 'processing',
  generated: 'success',
  revising: 'processing',
  failed: 'error',
}

export default function Workspaces() {
  const navigate = useNavigate()
  const { message } = App.useApp()
  const [list, setList] = useState<Workspace[]>([])
  const [loading, setLoading] = useState(false)

  const [newTitle, setNewTitle] = useState('')
  const [title, setTitle] = useState('')
  const [status, setStatus] = useState('')
  const [tag, setTag] = useState('')
  const [platform, setPlatform] = useState('')

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const params = new URLSearchParams()
      if (title) params.set('title', title)
      if (status) params.set('status', status)
      if (tag) params.set('tag', tag)
      if (platform) params.set('platform', platform)
      const data = (await api.get(`/workspaces?${params}`)) as any
      setList(data || [])
    } finally {
      setLoading(false)
    }
  }, [title, status, tag, platform])

  useEffect(() => {
    load()
  }, [load])

  const create = async () => {
    if (!newTitle.trim()) return
    try {
      await api.post('/workspaces', { title: newTitle.trim() })
      setNewTitle('')
      message.success('工作区已创建')
      load()
    } catch (e: any) {
      message.error(e.message || '创建失败')
    }
  }

  const remove = async (id: number) => {
    try {
      await api.delete(`/workspaces/${id}`)
      message.success('已删除')
      load()
    } catch (e: any) {
      message.error(e.message || '删除失败')
    }
  }

  const columns = [
    {
      title: '标题',
      dataIndex: 'title',
      render: (_: unknown, w: Workspace) => (
        <Typography.Link onClick={() => navigate(`/workspaces/${w.id}`)}>{w.title}</Typography.Link>
      ),
    },
    {
      title: '标签',
      dataIndex: 'requirement_tags',
      render: (tags?: string) =>
        tags ? tags.split(',').filter(Boolean).map((t) => <Tag key={t}>{t}</Tag>) : <span style={{ color: '#bbb' }}>—</span>,
    },
    {
      title: '状态',
      dataIndex: 'status',
      render: (s: string) => <Tag color={STATUS_COLOR[s] || 'default'}>{s}</Tag>,
    },
    {
      title: '操作',
      render: (_: unknown, w: Workspace) => (
        <Button danger size="small" icon={<DeleteOutlined />} onClick={() => remove(w.id)}>
          删除
        </Button>
      ),
    },
  ]

  return (
    <div>
      <Space style={{ marginBottom: 16 }} wrap>
        <Input
          placeholder="新建工作区标题"
          style={{ width: 240 }}
          value={newTitle}
          onChange={(e) => setNewTitle(e.target.value)}
          onPressEnter={create}
        />
        <Button type="primary" icon={<PlusOutlined />} onClick={create}>
          新建工作区
        </Button>
      </Space>

      <Space style={{ marginBottom: 16 }} wrap>
        <Input placeholder="标题" style={{ width: 120 }} value={title} onChange={(e) => setTitle(e.target.value)} />
        <Input placeholder="标签" style={{ width: 120 }} value={tag} onChange={(e) => setTag(e.target.value)} />
        <Input placeholder="平台" style={{ width: 120 }} value={platform} onChange={(e) => setPlatform(e.target.value)} />
        <Input placeholder="状态" style={{ width: 120 }} value={status} onChange={(e) => setStatus(e.target.value)} />
        <Button icon={<SearchOutlined />} onClick={load}>
          检索
        </Button>
      </Space>

      <Table
        rowKey="id"
        loading={loading}
        dataSource={list}
        columns={columns}
        pagination={{ pageSize: 20, showSizeChanger: false }}
      />
    </div>
  )
}
