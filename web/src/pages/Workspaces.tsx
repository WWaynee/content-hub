import { useCallback, useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import {
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
  Empty,
  Spin,
  Segmented,
  Tooltip,
  Card,
} from 'antd'
import type { CheckboxOptionType } from 'antd/es/checkbox/Group'
import {
  PlusOutlined,
  SearchOutlined,
  DeleteOutlined,
  ClockCircleOutlined,
  ArrowUpOutlined,
  ArrowDownOutlined,
  CloseCircleOutlined,
  FolderOpenOutlined,
  TagsOutlined,
} from '@ant-design/icons'
import api from '../api'
import type { Workspace } from '../types'
import { PLATFORMS } from '../types'

// 需求(工作区)状态彩色映射 —— 醒目提示
// 状态别名收敛：draft/needs_req 两个近义统一为「待填需求」（P12，避免卡片存在极少走到的 needs_req）。
const STATUS_META: { value: string; label: string; color: string }[] = [
  { value: 'draft', label: '待填需求', color: 'gold' },
  { value: 'generating', label: '生成中', color: 'blue' },
  { value: 'generated', label: '已生成', color: 'green' },
  { value: 'revising', label: '修改中', color: 'geekblue' },
  { value: 'failed', label: '生成失败', color: 'red' },
]

const STATUS_LABEL: Record<string, string> = Object.fromEntries(STATUS_META.map((s) => [s.value, s.label]))
const STATUS_COLOR: Record<string, string> = Object.fromEntries(STATUS_META.map((s) => [s.value, s.color]))
const STATUS_OPTIONS: CheckboxOptionType[] = STATUS_META.map((s) => ({
  label: s.label,
  value: s.value,
}))

function splitTags(t?: string): string[] {
  return t ? t.split(',').map((s) => s.trim()).filter(Boolean) : []
}

function fmtTime(t?: string): string {
  if (!t) return '—'
  const d = new Date(t)
  if (Number.isNaN(d.getTime())) return t
  return d.toLocaleString('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
}

export default function Workspaces() {
  const navigate = useNavigate()
  const { message } = App.useApp()
  const [list, setList] = useState<Workspace[]>([])
  const [loading, setLoading] = useState(false)

  // 标题搜索
  const [searchKeyword, setSearchKeyword] = useState('')
  const [appliedTitle, setAppliedTitle] = useState('')

  // 排序：时间正序/倒序
  const [sort, setSort] = useState<'time_desc' | 'time_asc'>('time_desc')
  // 状态快捷筛选（可多选，与排序组合）
  const [statuses, setStatuses] = useState<string[]>([])

  // 新建弹窗
  const [createOpen, setCreateOpen] = useState(false)
  const [creating, setCreating] = useState(false)
  const [createForm] = Form.useForm()
  // P10：起稿方式。from_scratch=从零生成（默认，沿用原表单校验）；assist=自带草稿起稿（放宽需求单必填）
  const [sourceKind, setSourceKind] = useState<'build_from_scratch' | 'draft_assist'>('build_from_scratch')
  const [draftInput, setDraftInput] = useState('')

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const params = new URLSearchParams()
      if (appliedTitle) params.set('title', appliedTitle)
      if (sort) params.set('sort', sort)
      statuses.forEach((s) => params.append('status', s))
      const qs = params.toString()
      const data = (await api.get(`/workspaces${qs ? `?${qs}` : ''}`)) as any
      setList(data || [])
    } finally {
      setLoading(false)
    }
  }, [appliedTitle, sort, statuses])

  useEffect(() => {
    load()
  }, [load])

  const doSearch = () => {
    setAppliedTitle(searchKeyword.trim())
  }
  const resetSearch = () => {
    setSearchKeyword('')
    setAppliedTitle('')
    setStatuses([])
    setSort('time_desc')
  }
  const hasFilter = !!appliedTitle || statuses.length > 0

  const openCreate = () => {
    createForm.resetFields()
    createForm.setFieldsValue({ platforms: [] })
    setSourceKind('build_from_scratch')
    setDraftInput('')
    setCreateOpen(true)
  }

  const create = async () => {
    const v = await createForm.validateFields()
    setCreating(true)
    try {
      await api.post('/workspaces', {
        title: v.title,
        req_title: sourceKind === 'draft_assist' && !v.req_title ? v.title : v.req_title,
        tags: (v.tags || []).filter((s: string) => s.trim()).map((s: string) => s.trim()),
        platforms: sourceKind === 'build_from_scratch' ? v.platforms || [] : v.platforms || [],
        style_tone: v.style_tone,
        style_emotion: v.style_emotion,
        style_audience: v.style_audience,
        style_purpose: v.style_purpose,
        style_subject: v.style_subject,
        word_count: v.word_count ? Number(v.word_count) : 0,
        chapter_requirement: v.chapter_requirement,
        source_kind: sourceKind,
        draft_input: sourceKind === 'draft_assist' ? draftInput.trim() : '',
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

  return (
    <div>
      {/* 头部：标题 + 靠右搜索 + 新建 */}
      <div className="page-header" style={{ marginBottom: 16 }}>
        <Typography.Title level={4} style={{ margin: 0 }}>
          工作区
        </Typography.Title>
        <Space wrap>
          <Space.Compact>
            <Input
              allowClear
              prefix={<SearchOutlined />}
              placeholder="按工作区标题搜索"
              style={{ width: 240 }}
              value={searchKeyword}
              onChange={(e) => setSearchKeyword(e.target.value)}
              onPressEnter={doSearch}
            />
            <Button type="primary" onClick={doSearch}>
              检索
            </Button>
          </Space.Compact>
          {hasFilter && (
            <Tooltip title="清除全部筛选">
              <Button icon={<CloseCircleOutlined />} onClick={resetSearch} />
            </Tooltip>
          )}
          <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>
            新建工作区
          </Button>
        </Space>
      </div>

      {/* 工具栏：时间排序 + 状态快捷筛选 */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          flexWrap: 'wrap',
          gap: 16,
          marginBottom: 16,
          padding: 12,
          borderRadius: 10,
          background: 'var(--panel-bg)',
          border: '1px solid var(--panel-border)',
        }}
      >
        <Space size={6}>
          <ClockCircleOutlined style={{ color: 'var(--text-soft)' }} />
          <Typography.Text type="secondary">时间</Typography.Text>
          <Segmented
            value={sort}
            onChange={(v) => setSort(v as 'time_desc' | 'time_asc')}
            options={[
              { label: '最新优先', value: 'time_desc', icon: <ArrowDownOutlined /> },
              { label: '最早优先', value: 'time_asc', icon: <ArrowUpOutlined /> },
            ]}
          />
        </Space>
        <Divider type="vertical" style={{ height: 24 }} />
        <Space size={6} wrap>
          <TagsOutlined style={{ color: 'var(--text-soft)' }} />
          <Typography.Text type="secondary">状态</Typography.Text>
          {STATUS_OPTIONS.map((o) => {
            const sel = statuses.includes(o.value as string)
            return (
              <Tag.CheckableTag
                key={o.value as string}
                checked={sel}
                style={sel ? { borderColor: 'transparent', background: STATUS_COLOR[o.value as string], color: '#fff' } : undefined}
                onChange={() =>
                  setStatuses((prev) =>
                    prev.includes(o.value as string) ? prev.filter((x) => x !== (o.value as string)) : [...prev, o.value as string],
                  )
                }
              >
                {o.label}
              </Tag.CheckableTag>
            )
          })}
        </Space>
      </div>

      {/* 卡片网格：动态列数，默认一行 3 个 */}
      <Spin spinning={loading}>
        {list.length === 0 ? (
          <Empty
            style={{ marginTop: 60 }}
            description={hasFilter ? '没有符合筛选条件的工作区' : '暂无工作区，点击右上角「新建工作区」开始'}
          />
        ) : (
          <div
            style={{
              display: 'grid',
              gridTemplateColumns: 'repeat(auto-fill, minmax(320px, 1fr))',
              gap: 16,
            }}
          >
            {list.map((w) => (
              <Card
                key={w.id}
                hoverable
                className="app-card"
                onClick={() => navigate(`/workspaces/${w.id}`)}
                style={{ cursor: 'pointer' }}
                styles={{ body: { padding: 16 } }}
              >
                {/* 状态标签（左上醒目）+ 删除 */}
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', marginBottom: 10 }}>
                  <Tag color={STATUS_COLOR[w.status] || 'default'} style={{ fontWeight: 600 }}>
                    {STATUS_LABEL[w.status] || w.status}
                  </Tag>
                  <Button
                    danger
                    size="small"
                    type="text"
                    icon={<DeleteOutlined />}
                    onClick={(e) => {
                      e.stopPropagation()
                      remove(w.id)
                    }}
                  />
                </div>

                {/* 工作区标题 */}
                <Typography.Title level={5} style={{ margin: '0 0 4px', wordBreak: 'break-word' }}>
                  <FolderOpenOutlined style={{ marginRight: 6, color: 'var(--accent)' }} />
                  {w.title}
                </Typography.Title>

                {/* 需求单标题 */}
                {w.requirement_title && (
                  <Typography.Text type="secondary" style={{ display: 'block', fontSize: 12 }}>
                    需求单：{w.requirement_title}
                    {w.requirement_version ? ` · v${w.requirement_version}` : ''}
                  </Typography.Text>
                )}

                {/* 需求单字段 */}
                <div style={{ marginTop: 10 }}>
                  {[
                    w.requirement_style_tone && `基调：${w.requirement_style_tone}`,
                    w.requirement_style_emotion && `色彩：${w.requirement_style_emotion}`,
                    w.requirement_style_audience && `受众：${w.requirement_style_audience}`,
                    w.requirement_style_subject && `主体：${w.requirement_style_subject}`,
                    w.requirement_style_purpose && `目的：${w.requirement_style_purpose}`,
                    w.requirement_word_count ? `字数：约${w.requirement_word_count}` : null,
                  ]
                    .filter(Boolean)
                    .slice(0, 3)
                    .map((line, i) => (
                      <Typography.Text key={i} type="secondary" style={{ display: 'block', fontSize: 12, lineHeight: '20px' }}>
                        {line}
                      </Typography.Text>
                    ))}
                  {w.requirement_chapter_requirement && (
                    <Typography.Text ellipsis style={{ display: 'block', fontSize: 12, color: 'var(--text-soft)' }} title={w.requirement_chapter_requirement}>
                      章节：{w.requirement_chapter_requirement}
                    </Typography.Text>
                  )}
                </div>

                {/* 标签 */}
                <div style={{ marginTop: 12, minHeight: 22 }}>
                  {splitTags(w.requirement_tags).map((t) => (
                    <Tag key={t} style={{ marginBottom: 4 }} color="blue">
                      {t}
                    </Tag>
                  ))}
                </div>

                {/* 更新时间 */}
                <Typography.Text type="secondary" style={{ display: 'block', marginTop: 10, fontSize: 12 }}>
                  更新于 {fmtTime(w.updated_at)}
                </Typography.Text>
              </Card>
            ))}
          </div>
        )}
      </Spin>

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
          <Form.Item label="工作区标题" name="title" rules={[{ required: true, message: '请输入工作区标题' }]}>
            <Input placeholder="如：招生简章发布稿" />
          </Form.Item>
          <Form.Item label="起稿方式" style={{ marginBottom: 8 }}>
            <Segmented
              block
              value={sourceKind}
              onChange={(v) => {
                setSourceKind(v as 'build_from_scratch' | 'draft_assist')
                if (v === 'build_from_scratch') setDraftInput('')
              }}
              options={[
                { label: '从零生成（填需求单）', value: 'build_from_scratch' },
                { label: '我有初稿要整理', value: 'draft_assist' },
              ]}
            />
            <Typography.Text type="secondary" style={{ display: 'block', fontSize: 12, marginTop: 6 }}>
              {sourceKind === 'draft_assist'
                ? 'P10：把你手上已有一份的同类稿子粘贴进来，系统会切分成句、逐句到知识库找可引依据（配得上→可溯源；配不上你又坚持的数据→以“用户草稿”黄点保留待你取舍），而不是把它当需求搜掉。'
                : '填好需求单后点“生成稿件”，从零生成一篇有据可溯的新稿。'}
            </Typography.Text>
          </Form.Item>

          {sourceKind === 'draft_assist' ? (
            <>
              <Form.Item
                label="粘贴你的草稿正文" style={{ marginBottom: 8 }}
                validateStatus={draftInput.trim() ? 'success' : undefined}
                extra="可与知识库资料互相印证/补漏。粘贴后仍可回到详情页做句子级修改与来源标注。"
              >
                <Input.TextArea
                  rows={7}
                  value={draftInput}
                  onChange={(e) => setDraftInput(e.target.value)}
                  placeholder="把你已经写好的稿件/模板/往期范文原文粘到这里，例如：{换行换行}新年致辞 某单位2024年工作总结…"
                />
              </Form.Item>
            </>
          ) : (
            <>
              <Divider style={{ margin: '12px 0' }}>需求单初步内容</Divider>
              <Form.Item label="需求单标题" name="req_title" rules={[{ required: true, message: '请输入需求单标题' }]}>
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
                <Form.Item label="发布平台" name="platforms" rules={[{ required: true, message: '请选择至少一个平台' }]}>
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
            </>
          )}
        </Form>
      </Modal>
    </div>
  )
}
