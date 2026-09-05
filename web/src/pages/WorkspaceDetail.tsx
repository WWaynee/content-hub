import { useCallback, useEffect, useRef, useState } from 'react'
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
  Segmented,
  Tooltip,
  Steps,
} from 'antd'
import { SendOutlined, FolderOpenOutlined, LoadingOutlined, CheckCircleOutlined, CloseCircleOutlined } from '@ant-design/icons'
import api from '../api'
import { openGenerateStream } from '../sse'
import type { Requirement, Article } from '../types'
import RequirementScopeModal from './RequirementScopeModal'
import ArticleSentencesBoard from './ArticleSentencesBoard'
import ArticleReadableView from './ArticleReadableView'
import { isRequirementReady, requirementMissing } from '../guide'
import { PLATFORMS } from '../types'

// P13：生成进度显示的 4 张语义步骤卡标题 + 全局常量（供组件内排布面板与状态机）。
const GEN_TITLES = ['解析需求并在知识库检索证据', '撰写全文', '逐句核实数据断言有据可查', '整理证据清单并成稿']

type GenStepState = {
  no: number
  status: 'pending' | 'running' | 'done' | 'failed'
  detail?: string
  failure?: string
}

export default function WorkspaceDetail() {
  const { id } = useParams()
  const wid = Number(id)
  const { message } = App.useApp()

  const [req, setReq] = useState<Requirement | null>(null)
  const [article, setArticle] = useState<Article | null>(null)
  const [generating, setGenerating] = useState(false)
  // P13：生成进度（SSE 流式：前端实时拿增量逐步点亮 4 张语义步卡）。
  const [genStepNow, setGenStepNow] = useState<string>('')
  const [genSteps, setGenSteps] = useState<GenStepState[]>([])
  const [genFinal, setGenFinal] = useState<{ ok: boolean; text: string } | null>(null)
  const genStepsRef = useRef<GenStepState[]>([])
  // P11：稿件页签视图——read=可读正文（默认）；edit=逐句受控编辑面板
  const [articleView, setArticleView] = useState<'read' | 'edit'>('read')
  // P12：最近一次对话的结果（人话清单，不使用 tool:xxx）
  const [chatLog, setChatLog] = useState<{ ok: boolean; text: string }[] | null>(null)
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

  // patchStep：按序号就地更新一张语义进度卡（不可变更新 + ref 同步，供事件回调实时用）。
  const patchStep = (no: number, p: Partial<GenStepState>) => {
    const cur = genStepsRef.current
    const idx = cur.findIndex((s) => s.no === no)
    const next =
      idx === -1 ? [...cur, { no, status: (p.status as GenStepState['status']) || 'pending', ...p }] : cur.map((s, i) => (i === idx ? { ...s, ...p } : s))
    genStepsRef.current = next
    setGenSteps(next)
  }

  // P13：生成走 SSE 流式——POST 返回 run_id 后立即连全进程事件的流，逐步点亮 4 张卡，绝不轮询。
  const generate = async () => {
    setChatLog(null)
    if (req && !isRequirementReady(req)) {
      message.warning('还不能一键生成，需求单还缺：' + requirementMissing(req).join('、'))
      return
    }
    setGenerating(true)
    setGenStepNow('')
    setGenFinal(null)
    genStepsRef.current = []
    setGenSteps([])
    try {
      const posted: any = await api.post(`/workspaces/${wid}/generate`)
      const runId = Number(posted?.run_id)
      const out = await new Promise<{ ok: boolean; text: string }>((resolve) => {
        if (!runId) {
          resolve({ ok: false, text: '服务未返回任务号，请重试' })
          return
        }
        const close = openGenerateStream(wid, runId, (ev) => {
          switch (ev.type) {
            case 'step_begin':
              patchStep(ev.stepNo, { status: 'running' })
              break
            case 'step_detail':
              patchStep(ev.stepNo, { status: 'running', detail: ev.detail || '' })
              break
            case 'step_done':
              patchStep(ev.stepNo, { status: 'done', detail: ev.detail || '' })
              break
            case 'step_fail': {
              patchStep(ev.stepNo, { status: 'failed', failure: ev.failure })
              resolve({ ok: false, text: ev.failure || '该步骤未通过' })
              break
            }
            case 'run_done':
              resolve({ ok: true, text: '稿件已生成' })
              break
            case 'run_failed':
              resolve({ ok: false, text: String((ev as any).payload || '生成失败') })
              break
          }
        }, (err) => resolve({ ok: false, text: err || '进度连接中断' }))
        // 兜底超时：别让按钮一直转（Promise 已 resolve 后此 resolve 被忽略）。
        setTimeout(() => {
          close()
          resolve({ ok: false, text: '生成超时，请稍后刷新，服务器可能仍在后台处理' })
        }, 600000)
      })
      if (out.ok) {
        await loadArticle()
        setGenStepNow('')
        setGenFinal({ ok: true, text: out.text })
        message.success(out.text)
      } else {
        setGenStepNow('')
        setGenFinal({ ok: false, text: out.text })
        message.error(out.text.length > 500 ? out.text.slice(0, 500) + '…' : out.text)
      }
    } catch (e: any) {
      setGenFinal({ ok: false, text: '生成失败: ' + (e?.message || '') })
      message.error('生成失败: ' + (e?.message || ''))
    } finally {
      setGenerating(false)
    }
  }

  // P10 draft_assist：从用户粘贴的草稿起稿（切分→逐句找可引依据→落首版）。
  const [draftAssisting, setDraftAssisting] = useState(false)
  const [draftText, setDraftText] = useState('')
  const generateFromDraftAssist = async () => {
    const text = draftText.trim()
    if (!text && !(req?.draft_input ?? '').trim()) {
      message.warning('请先粘贴你的草稿正文（或先在需求单中填草稿）')
      return
    }
    setDraftAssisting(true)
    try {
      const r: any = await api.post(`/workspaces/${wid}/draft-assist`, { text })
      message.info(r?.human_text || '已从草稿整理成新稿')
      await loadArticle()
      await loadReq()
    } catch (e: any) {
      message.error('从草稿起稿失败: ' + (e.message || ''))
      await loadReq()
    } finally {
      setDraftAssisting(false)
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
      const raw: any[] = r?.results || []
      if (raw.length === 0) {
        setChatLog([{ ok: true, text: '这条消息我没有修改任何需求单/稿件。如需改某个字段、追加段落或补检索，跟我说即可。' }])
      } else {
        setChatLog(
          raw.map((x) => ({
            ok: !!x.success,
            text: x.human_text || (x.success ? '已处理。' : '未能完成，换个说法或到对应板块手动处理。'),
          })),
        )
      }
      setChat('')
      loadReq()
    } catch (e: any) {
      message.error('对话失败: ' + (e.message || ''))
    } finally {
      setSending(false)
    }
  }

  // P12/W4：可生成派生态（禁态 + 缺项 tooltip + 向导步）
  const canGen = !!req && isRequirementReady(req)
  const reqMissing = req ? requirementMissing(req) : []
  const genBlockReason = reqMissing.length ? '生成稿件前还缺：' + reqMissing.join('、') : ''

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
            generateAllowed={canGen}
            generateHint={genBlockReason}
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
            generateAllowed={canGen}
            generateHint={genBlockReason}
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
          <Divider titlePlacement="left">稿件呈现</Divider>
          <Segmented
            value={articleView}
            onChange={(v) => setArticleView(v as 'read' | 'edit')}
            options={[
              { label: '可读正文（看逐句出处）', value: 'read' },
              { label: '逐句编辑 / 取舍', value: 'edit' },
            ]}
            block
            style={{ marginBottom: 14 }}
          />
          {articleView === 'read' ? (
            <ArticleReadableView
              article={article}
              onGoEditNoSource={() => setArticleView('edit')}
            />
          ) : (
            <>
              <ArticleSentencesBoard
                wid={wid}
                article={article}
                onCommitted={loadArticle}
              />
              <Button size="small" style={{ marginTop: 8 }} onClick={() => setArticleView('read')}>
                回到可读正文
              </Button>
            </>
          )}
        </div>
      ) : req?.source_kind === 'draft_assist' ? (
        <div style={{ maxWidth: 640 }}>
          <Typography.Paragraph type="secondary">
            这个工作区是「拿已有初稿整理」类型：把你有的一份同类稿子粘进来，系统会切成句子、逐句到知识库找可引依据——
            能配上就标为可溯源；配上不了、又是你坚持要保留的数据/措辞，会以“用户草稿·待复核”黄点保留，不会再把它当需求搜掉。
            落稿后你仍可在下方句子面板里逐句受控修改与取舍。
          </Typography.Paragraph>
          <Form.Item
            label="草稿正文"
            validateStatus={draftText.trim() || (req?.draft_input ?? '').trim() ? 'success' : undefined}
            extra="系统会把草稿切成完整句，逐句与已勾选/可访问的知识库资料比对。"
          >
            <Input.TextArea
              rows={8}
              value={draftText}
              onChange={(e) => setDraftText(e.target.value)}
              placeholder={req?.draft_input?.trim() ? '（已保存过草稿，可直接点击下方起稿；此处可改后覆盖）先用知识库帮我整理这份：' : '粘贴你的草稿：'}
            />
          </Form.Item>
          <Button
            type="primary"
            icon={<SendOutlined />}
            loading={draftAssisting}
            onClick={generateFromDraftAssist}
          >
            从这份草稿整理成稿
          </Button>
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

      {req || article ? (
        <div style={{ marginBottom: 12 }}>
          <Steps
            size="small"
            current={article ? 2 : canGen ? 1 : 0}
            items={[
              { title: '1 填需求', description: canGen ? '需求已具备' : '还缺：' + (reqMissing.slice(0, 2).join('、') || '需求数据') },
              {
                title: '2 生成稿件',
                description: generating ? (genStepNow || '正在生成') : canGen ? '点「生成稿件」，会先去资料里逐句找证据' : '补齐需求后可一键生成',
              },
              { title: '3 看稿 / 导出', description: article ? '稿件页签可读正文并导出含证据清单' : '有新稿后去“稿件”页签查看' },
            ]}
          />
        </div>
      ) : null}

      {/* P13：稿件生成进度（SSE）面板——用户在生成时可逐步看到每一步在干嘛/是否失败 */}
      {(generating || genSteps.length > 0 || genFinal) ? (
        <div
          style={{
            margin: '4px 0 12px',
            padding: '10px 14px',
            border: '1px solid var(--border, #eee)',
            borderRadius: 8,
            background: 'var(--panel-bg, #fafafa)',
          }}
        >
          <Typography.Text type="secondary" style={{ fontSize: 12, display: 'block', marginBottom: 6 }}>
            {genFinal ? '本次生成结果' : generating ? '正在生成稿件…' : '最近一次生成进度'}
          </Typography.Text>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
            {GEN_TITLES.map((title, i) => {
              const no = i + 1
              const st = genSteps.find((s) => s.no === no)
              const status = st?.status || 'pending'
              return (
                <div key={no} style={{ display: 'flex', alignItems: 'flex-start', gap: 8 }}>
                  <span style={{ width: 20, marginTop: 2 }}>
                    {status === 'running' ? (
                      <LoadingOutlined spin style={{ color: '#1677ff' }} />
                    ) : status === 'done' ? (
                      <CheckCircleOutlined style={{ color: '#52c41a' }} />
                    ) : status === 'failed' ? (
                      <CloseCircleOutlined style={{ color: '#ff4d4f' }} />
                    ) : (
                      <span style={{ width: 12, height: 12, display: 'inline-block', marginLeft: 2, border: '1px solid #bbb', borderRadius: '50%' }} />
                    )}
                  </span>
                  <div>
                    <Typography.Text strong={status === 'running'} type={st?.failure ? 'danger' : undefined}>
                      {no}. {title}
                    </Typography.Text>
                    {st?.failure ? (
                      <div style={{ color: '#ff4d4f', whiteSpace: 'pre-wrap' }}>{st.failure}</div>
                    ) : st?.detail ? (
                      <Typography.Text type="secondary" style={{ display: 'block' }}>
                        <span style={{ opacity: 0.5 }}>↳ </span>
                        {st.detail}
                      </Typography.Text>
                    ) : null}
                  </div>
                </div>
              )
            })}
          </div>
          {genFinal && (
            <div style={{ marginTop: 8, color: genFinal.ok ? '#52c41a' : '#ff4d4f', fontWeight: 500, whiteSpace: 'pre-wrap' }}>
              {genFinal.ok ? '✓ ' : '✕ '}
              {genFinal.text.length > 900 ? genFinal.text.slice(0, 900) + '…' : genFinal.text}
            </div>
          )}
        </div>
      ) : null}

      <Tabs items={tabItems} />

      <Divider titlePlacement="left">需求对话（说出你想让 AI 帮你改的需求/稿件动作）</Divider>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 8, maxWidth: 640 }}>
        {chatLog && chatLog.length > 0 ? (
          <div
            style={{
              padding: '8px 12px',
              background: 'var(--panel-bg, #fff)',
              border: '1px solid var(--border, #eee)',
              borderRadius: 8,
            }}
          >
            {chatLog.map((l, i) => (
              <div key={i} style={{ display: 'flex', gap: 8, padding: '2px 0' }}>
                <span style={{ color: l.ok ? '#52c41a' : '#cf1322', flex: '0 0 auto' }}>{l.ok ? '✓' : '✕'}</span>
                <Typography.Text style={{ whiteSpace: 'pre-wrap' }}>{l.text}</Typography.Text>
              </div>
            ))}
          </div>
        ) : null}
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
  generateAllowed?: boolean
  generateHint?: string
}) {
  const genAllowed = props.generateAllowed !== false
  return (
    <div style={{ marginBottom: 16 }}>
      {props.onGenerate && (
        <Tooltip title={!genAllowed ? props.generateHint || '需求还需补充后才能生成' : undefined}>
          <span>
            <Button
              type="primary"
              loading={props.generating}
              disabled={!genAllowed}
              onClick={props.onGenerate}
              style={{ marginRight: 8 }}
            >
              生成稿件
            </Button>
          </span>
        </Tooltip>
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
