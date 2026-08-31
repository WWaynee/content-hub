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
  Tag,
  Card,
  Typography,
  Divider,
} from 'antd'
import { SendOutlined } from '@ant-design/icons'
import api from '../api'
import type { Requirement, Article } from '../types'
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
  const [form] = Form.useForm()

  const loadReq = useCallback(async () => {
    const r = (await api.get(`/workspaces/${wid}/requirement`)) as any
    setReq(r)
    form.setFieldsValue({
      title: r.title,
      tags: (r.tags || []).join(','),
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
        tags: (v.tags || '').split(',').map((s: string) => s.trim()).filter(Boolean),
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
          <Form.Item label="标签（逗号分隔）" name="tags">
            <Input placeholder="如：招生, 政策" />
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
            <Card size="small" style={{ marginBottom: 16, background: '#fafafa' }}>
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
          <Divider titlePlacement="left">句子（含证据）</Divider>
          {article.sentences.map((s) => {
            const bindings = article.bindings.filter((b) => b.article_sentence_id === s.id)
            return (
              <div key={s.id} style={{ marginBottom: 8 }}>
                <Typography.Text>{s.content}</Typography.Text>
                {bindings.length > 0 && (
                  <Tag color="blue" style={{ marginLeft: 8 }}>
                    证据 x{bindings.length}
                  </Tag>
                )}
              </div>
            )
          })}
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
