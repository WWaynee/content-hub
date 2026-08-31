import { useCallback, useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  Table,
  Input,
  Button,
  Space,
  App,
  Tag,
  Typography,
  Select,
  Modal,
  Form,
  Divider,
  Tooltip,
} from 'antd'
import {
  PlusOutlined,
  SearchOutlined,
  DeleteOutlined,
  FilterOutlined,
  CloseCircleOutlined,
} from '@ant-design/icons'
import api from '../api'
import type { Workspace } from '../types'
import { PLATFORMS } from '../types'

const STATUS_COLOR: Record<string, string> = {
  draft: 'default',
  needs_req: 'warning',
  generating: 'processing',
  generated: 'success',
  revising: 'processing',
  failed: 'error',
}

const SEARCH_FIELDS = [
  { label: '标题', value: 'title' },
  { label: '标签', value: 'tag' },
  { label: '平台', value: 'platform' },
  { label: '状态', value: 'status' },
]

export default function Workspaces() {
  const navigate = useNavigate()
  const { message } = App.useApp()
  const [list, setList] = useState<Workspace[]>([])
  const [loading, setLoading] = useState(false)

  // 搜索：下拉字段 + 单输入 + 检索
  const [searchField, setSearchField] = useState('title')
  const [searchKeyword, setSearchKeyword] = useState('')
  const [appliedQuery, setAppliedQuery] = useState<Record<string, string>>({})

  // 新建弹窗
  const [createOpen, setCreateOpen] = useState(false)
  const [creating, setCreating] = useState(false)
  const [createForm] = Form.useForm()

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const params = new URLSearchParams()
      Object.entries(appliedQuery).forEach(([k, v]) => {
        if (v) params.set(k, v)
      })
      const data = (await api.get(`/workspaces?${params}`)) as any
      setList(data || [])
    } finally {
      setLoading(false)
    }
  }, [appliedQuery])

  useEffect(() => {
    load()
  }, [load])

  const doSearch = () => {
    if (!searchKeyword.trim()) {
      setAppliedQuery({})
    } else {
      setAppliedQuery({ [searchField]: searchKeyword.trim() })
    }
  }

  const resetSearch = () => {
    setSearchKeyword('')
    setAppliedQuery({})
  }

  const openCreate = () => {
    createForm.resetFields()
    createForm.setFieldsValue({ platforms: [] })
    setCreateOpen(true)
  }

  const create = async () => {
    const v = await createForm.validateFields()
    setCreating(true)
    try {
      await api.post('/workspaces', {
        title: v.title,
        req_title: v.req_title,
        tags: (v.tags || []).filter((s: string) => s.trim()).map((s: string) => s.trim()),
        platforms: v.platforms || [],
        style_tone: v.style_tone,
        style_emotion: v.style_emotion,
        style_audience: v.style_audience,
        style_purpose: v.style_purpose,
        style_subject: v.style_subject,
        word_count: v.word_count ? Number(v.word_count) : 0,
        chapter_requirement: v.chapter_requirement,
      })
      message.success('工作区已创建')
      setCreateOpen(false)
      load()
    } catch (e: any) {
      message.error(e.message || '创建失败')
    } finally {
      setCreating(false)
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
        tags
          ? tags.split(',').filter(Boolean).map((t) => <Tag key={t}>{t}</Tag>)
          : <span style={{ color: 'var(--text-soft)' }}>—</span>,
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
      {/* 头部：标题 + 靠右搜索 + 新建 */}
      <div className="page-header">
        <Typography.Title level={4} style={{ margin: 0 }}>
          工作区
        </Typography.Title>
        <Space>
          <Space.Compact>
            <Select
              value={searchField}
              onChange={setSearchField}
              style={{ width: 96 }}
              options={SEARCH_FIELDS}
              suffixIcon={<FilterOutlined />}
            />
            <Input
              allowClear
              placeholder="输入关键字"
              style={{ width: 220 }}
              value={searchKeyword}
              onChange={(e) => setSearchKeyword(e.target.value)}
              onPressEnter={doSearch}
            />
            <Button type="primary" icon={<SearchOutlined />} onClick={doSearch}>
              检索
            </Button>
          </Space.Compact>
          {Object.keys(appliedQuery).length > 0 && (
            <Tooltip title="清除搜索">
              <Button icon={<CloseCircleOutlined />} onClick={resetSearch} />
            </Tooltip>
          )}
          <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>
            新建工作区
          </Button>
        </Space>
      </div>

      <Table
        rowKey="id"
        loading={loading}
        dataSource={list}
        columns={columns}
        pagination={{ pageSize: 20, showSizeChanger: false }}
        locale={{ emptyText: '暂无工作区，点击右上角「新建工作区」开始' }}
      />

      {/* 新建工作区弹窗（工作区 + 需求单初步内容） */}
      <Modal
        title="新建工作区"
        open={createOpen}
        onOk={create}
        confirmLoading={creating}
        onCancel={() => setCreateOpen(false)}
        okText="创建"
        width={560}
      >
        <Form form={createForm} layout="vertical">
          <Form.Item
            label="工作区标题"
            name="title"
            rules={[{ required: true, message: '请输入工作区标题' }]}
          >
            <Input placeholder="如：招生简章发布稿" />
          </Form.Item>
          <Divider style={{ margin: '12px 0' }}>需求单初步内容</Divider>
          <Form.Item
            label="需求单标题"
            name="req_title"
            rules={[{ required: true, message: '请输入需求单标题' }]}
          >
            <Input placeholder="如：招生简章发布稿" />
          </Form.Item>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
            <Form.Item label="标签" name="tags" extra="输入后回车添加，可多个；支持逗号智能分隔">
              <Select
                mode="tags"
                placeholder="输入标签后回车添加（如：招生）"
                tokenSeparators={[',', '，']}
                options={[
                  { label: '招生', value: '招生' },
                  { label: '政策', value: '政策' },
                  { label: '通知', value: '通知' },
                  { label: '公告', value: '公告' },
                  { label: '活动', value: '活动' },
                  { label: '宣传', value: '宣传' },
                ]}
              />
            </Form.Item>
            <Form.Item
              label="发布平台"
              name="platforms"
              rules={[{ required: true, message: '请选择至少一个平台' }]}
            >
              <Select mode="multiple" placeholder="选择发布平台" options={PLATFORMS.map((p) => ({ label: p, value: p }))} />
            </Form.Item>
          </div>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
            <Form.Item label="基调" name="style_tone">
              <Input placeholder="如：正式" />
            </Form.Item>
            <Form.Item label="感情色彩" name="style_emotion">
              <Input placeholder="如：积极" />
            </Form.Item>
            <Form.Item label="目标受众" name="style_audience">
              <Input placeholder="如：考生及家长" />
            </Form.Item>
            <Form.Item label="发文主体" name="style_subject">
              <Input placeholder="如：学校" />
            </Form.Item>
          </div>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
            <Form.Item label="字数要求" name="word_count">
              <Input type="number" placeholder="如：800" />
            </Form.Item>
            <Form.Item label="章节要求" name="chapter_requirement">
              <Input placeholder="如：含报名条件和录取规则" />
            </Form.Item>
          </div>
          <Form.Item label="发文目的" name="style_purpose">
            <Input placeholder="如：发布招生政策" />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  )
}
