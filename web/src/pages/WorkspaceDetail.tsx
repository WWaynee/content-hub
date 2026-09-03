import { useCallback, useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import {
  Tabs,
  Form,
  Input,
  Select,
  Button,
  App,
  Descriptions,
  Card,
  Typography,
  Divider,
} from 'antd'
import { SendOutlined, FolderOpenOutlined } from '@ant-design/icons'
import api from '../api'
import type { Requirement, Article } from '../types'
import RequirementScopeModal from './RequirementScopeModal'
import ArticleSentencesBoard from './ArticleSentencesBoard'
import { PLATFORMS } from '../types'

export default function WorkspaceDetail() {
  const { id } = useParams()
  const wid = Number(id)
  const { message } = App.useApp()

  const [req, setReq] = useState<Requirement | null>(null)
  const [article, setArticle] = useState<Article | null>(null)
  const [generating, setGenerating] = useState(false)
  const [exported, setExported] = useState('')
  const [chat, setChat] = useState('')
  const [sending, setSending] = useState(false)
  const [scopeVisible, setScopeVisible] = useState(false)
  const [scopeCount, setScopeCount] = useState(0)
  const [form] = Form.useForm()

  const refreshScopeCount = useCallback(async (rid: number) => {
    try {
      const list = (await api.get(`/requirements/${rid}/scope`)) as any
      setScopeCount(Array.isArray(list) ? list.length : 0)
    } catch (_) {
      setScopeCount(0)
    }
  }, [])

  const loadReq = useCallback(async () => {
    const r = (await api.get(`/workspaces/${wid}/requirement`)) as any
    setReq(r)
    if (r?.id) refreshScopeCount(r.id)
    form.setFieldsValue({
      title: r.title,
      tags: r.tags || [],
      platforms: r.platforms || [],
      style_tone: r.style_tone,
      style_emotion: r.style_emotion,
      style_audience: r.style_audience,
      style_purpose: r.style_purpose,
      style_subject: r.style_subject,
      style_taboo: r.style_taboo,
      word_count: r.word_count,
      chapter_requirement: r.chapter_requirement,
    })
  }, [wid, form])
  const loadArticle = useCallback(async () => {
    try {
      const a = (await api.get(`/workspaces/${wid}/article`)) as any
      setArticle(a)
    } catch {
      setArticle(null)
    }
  }, [wid])

  useEffect(() => {
    loadReq()
    loadArticle()
  }, [loadReq, loadArticle])

  const saveReq = async () => {
    if (!req) return
    const v = form.getFieldsValue()
    try {
      await api.put(`/requirements/${req.id}`, {
        ...req,
        title: v.title,
        tags: (v.tags || []).filter((s: string) => s.trim()).map((s: string) => s.trim()),
        platforms: v.platforms || [],
        style_tone: v.style_tone,
        style_emotion: v.style_emotion,
        style_audience: v.style_audience,
        style_purpose: v.style_purpose,
        style_subject: v.style_subject,
        style_taboo: v.style_taboo,
        word_count: Number(v.word_count),
        chapter_requirement: v.chapter_requirement,
      })
      message.success('需求单已保存')
      loadReq()
    } catch (e: any) {
      message.error(e.message || '保存失败')
    }
  }

  const generate = async () => {
    setGenerating(true)
    try {
      await api.post(`/workspaces/${wid}/generate`)
      await loadArticle()
      message.success('稿件已生成')
    } catch (e: any) {
      message.error('生成失败: ' + (e.message || ''))
    } finally {
      setGenerating(false)
    }
  }

  const doExport = async () => {
    if (!article) return
    const r = (await api.get(`/articles/${article.article_version_id}/export`)) as any
    setExported(r.markdown)
    message.success('已导出')
  }

  const sendChat = async () => {
    if (!chat || !req) return
    setSending(true)
    try {
      const r = (await api.post(`/workspaces/${wid}/chat`, {
        message: chat,
        target_type: 'requirement_field',
        target_ref: req.id,
      })) as any
      const summary = (r?.results || [])
        .map((x: any) => `${x.tool}:${x.success ? '成功' : '失败'}(${x.message})`)
        .join('\n')
      message.info('对话处理结果：\n' + (summary || '无动作'))
      setChat('')
      loadReq()
    } catch (e: any) {
      message.error('对话失败: ' + (e.message || ''))
    } finally {
      setSending(false)
    }
  }

  const tabItems = [
    {
      key: 'requirement',
      label: '需求单',
      children: (
        <Form
          form={form}
          layout="vertical"
          style={{ maxWidth: 640 }}
          initialValues={{ word_count: 0 }}
        >
          <Form.Item label="标题" name="title" rules={[{ required: true, message: '请填写标题' }]}>
            <Input />
          </Form.Item>
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
          <Form.Item label="发布平台" name="platforms">
            <Select mode="multiple" allowClear placeholder="选择发布平台" options={PLATFORMS.map((p) => ({ label: p, value: p }))} />
          </Form.Item>
          <Form.Item label="基调" name="style_tone">
            <Input placeholder="如：正式" />
          </Form.Item>
          <Form.Item label="感情色彩" name="style_emotion">
            <Input placeholder="如：积极" />
          </Form.Item>
          <Form.Item label="目标受众" name="style_audience">
            <Input placeholder="如：考生及家长" />
          </Form.Item>
          <Form.Item label="发文目的" name="style_purpose">
            <Input placeholder="如：发布招生政策" />
          </Form.Item>
          <Form.Item label="发文主体" name="style_subject">
            <Input placeholder="如：学校" />
          </Form.Item>
          <Form.Item label="禁忌/约束" name="style_taboo">
            <Input.TextArea rows={2} />
          </Form.Item>
          <Form.Item label="字数要求" name="word_count">
            <Input type="number" />
          </Form.Item>
          <Form.Item label="章节要求" name="chapter_requirement">
            <Input.TextArea rows={3} placeholder="如：包含报名条件和录取规则" />
          </Form.Item>
          <Form.Item label="引用资料范围" tooltip="锁定稿件检索仅来自勾选的目录/文件；目录含其下所有文件与子目录。不勾选则检索全部可访问资料。">
            <div style={{ display: 'flex', alignItems: 'center', gap: 16, flexWrap: 'wrap' }}>
              <Typography.Text type="secondary">
                {scopeCount > 0
                  ? `已选择 ${scopeCount} 项引用资料`
                  : '尚未选择，将引用全部可访问资料'}
              </Typography.Text>
              <Button icon={<FolderOpenOutlined />} disabled={!req} onClick={() => setScopeVisible(true)}>
                {scopeCount > 0 ? '查看 / 修改范围' : '选择资料（推荐锁死范围）'}
              </Button>
            </div>
          </Form.Item>
          <SpaceCombo
            onSave={saveReq}
            onGenerate={generate}
            generating={generating}
          />
        </Form>
      ),
    },
    {
      key: 'article',
      label: '稿件',
      children: article ? (
        <div>
          <SpaceCombo
            onExport={doExport}
            onGenerate={generate}
            generating={generating}
          />
          {exported && (
            <Card size="small" style={{ marginBottom: 16, background: 'var(--panel-bg)' }}>
              <Typography.Paragraph
                style={{ whiteSpace: 'pre-wrap' }}
                copyable={{ text: exported }}
              >
                {exported}
              </Typography.Paragraph>
            </Card>
          )}
          <Divider titlePlacement="left">正文</Divider>
          <Typography.Paragraph style={{ whiteSpace: 'pre-wrap' }}>{article.full_content}</Typography.Paragraph>
          <Divider titlePlacement="left">句子 · 受控编辑 / 可溯源</Divider>
          <ArticleSentencesBoard
            wid={wid}
            article={article}
            onCommitted={loadArticle}
          />
        </div>
      ) : (
        <Typography.Text type="secondary">
          尚未生成稿件，请先在「需求单」页签填好需求并点击「生成稿件」。
        </Typography.Text>
      ),
    },
  ]

  return (
    <div>
      <Descriptions size="small" style={{ marginBottom: 16 }}>
        <Descriptions.Item label="工作区 ID">#{wid}</Descriptions.Item>
        {req && (
          <>
            <Descriptions.Item label="版本">v{req.version}</Descriptions.Item>
            <Descriptions.Item label="需求标题">{req.title || '—'}</Descriptions.Item>
          </>
        )}
      </Descriptions>

      <Tabs items={tabItems} />

      <Divider titlePlacement="left">需求对话（AI 修改需求单字段）</Divider>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 8, maxWidth: 640 }}>
        <Input.TextArea
          rows={3}
          placeholder="如：把基调改成严谨"
          value={chat}
          onChange={(e) => setChat(e.target.value)}
        />
        <Button
          type="primary"
          icon={<SendOutlined />}
          loading={sending}
          onClick={sendChat}
          style={{ alignSelf: 'flex-end' }}
        >
          发送
        </Button>
      </div>

      <RequirementScopeModal
        open={scopeVisible && !!req}
        requirementId={req?.id || 0}
        onClose={() => setScopeVisible(false)}
        onSaved={() => {
          if (req?.id) refreshScopeCount(req.id)
          loadReq()
        }}
      />
    </div>
  )
}

function SpaceCombo(props: {
  onSave?: () => void
  onGenerate?: () => void
  onExport?: () => void
  generating?: boolean
}) {
  return (
    <div style={{ marginBottom: 16 }}>
      {props.onGenerate && (
        <Button type="primary" loading={props.generating} onClick={props.onGenerate} style={{ marginRight: 8 }}>
          生成稿件
        </Button>
      )}
      {props.onSave && (
        <Button onClick={props.onSave} style={{ marginRight: 8 }}>
          保存需求单
        </Button>
      )}
      {props.onExport && <Button onClick={props.onExport}>导出（含证据清单）</Button>}
    </div>
  )
}
