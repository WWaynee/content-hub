/**
 * ArticleReadableView — P11 的可读正文视图（逐句出处“看得见”）。
 *
 * WorkspaceDetail 的「稿件」页签里：默认“可读正文”，切到可读区可逐句看来源；
 * 需要动手(改/插/删/取舍)再切“逐句编辑”(ArticleSentencesBoard)。
 *
 * 原则：
 *   - 段按 P11 选择的“段级分组 / 朴素序正文”排布（后端 sections 无标题字段，不强造标题树）。
 *   - 每句以 sentence_id 桥到 P04 sentence_views：
 *        bound        → 低调 ⓘ，悬浮看出处卡片（原文/文档/章节/版本/已有新版/复制引文）
 *        no_source    → 黄色“待核”标，点击可切去逐句编辑（做认可/删除）；user_draft 语义一致
 *        human_kept   → 绿标“人工保留”，不打扰
 *        plausible    → 通稿句，无额外标
 *   - 页级：只看/淡化无据句；折叠“证据密度 / 出处”清单（默认收起）。
 */
import { useMemo, useState } from 'react'
import { Button, Collapse, Space, Switch, Tooltip, Typography } from 'antd'
import { BookOutlined, EyeOutlined } from '@ant-design/icons'
import type { Article, SentenceView } from '../types'
import { BoundSourceMarker, NoSourceMark, SourcesCard } from './ArticleSourceTooltip'

/** 一行句子的渲染工作稿（view 可缺省即无 P04 source 的旧数据/线性兜底）。 */
type Row = { id: number; text: string; view?: SentenceView }

export default function ArticleReadableView(props: { article: Article; onGoEditNoSource?: () => void }) {
  const { article } = props

  const viewById: Map<number, SentenceView> = useMemo(() => {
    const m = new Map<number, SentenceView>()
    for (const v of article.sentence_views ?? []) m.set(v.sentence_id, v)
    return m
  }, [article.sentence_views])

  const rows: Row[] = useMemo(() => {
    const out: Row[] = []
    // 结构化 sections → 段落组
    for (const sec of article.sections ?? []) {
      for (const p of sec.paragraphs) {
        for (const s of p.sentences) observe(out, s.id, s.content, viewById)
      }
    }
    // 兜底：sections 缺失(旧线性稿)时按 sentence_views / sentences 重建
    if (out.length === 0) {
      for (const v of article.sentence_views ?? []) observe(out, v.sentence_id, v.text, viewById)
    }
    if (out.length === 0) {
      for (const s of article.sentences ?? []) observe(out, s.id, s.content, viewById)
    }
    return out
  }, [article.sections, article.sentence_views, article.sentences, viewById])

  const noSourceCount = useMemo(() => rows.filter((r) => r.view?.claim_type === 'no_source').length, [rows])

  const isBound = (r: Row) => !!r.view && r.view.claim_type === 'bound' && (r.view.sources?.length ?? 0) > 0
  const claimOf = (r: Row) => r.view?.claim_type

  const [onlyBound, setOnlyBound] = useState(false)
  const [refOpen, setRefOpen] = useState(false)

  const boundRows: Row[] = rows.filter(isBound)

  return (
    <div>
      <div style={{ display: 'flex', gap: 12, alignItems: 'center', flexWrap: 'wrap', marginBottom: 10 }}>
        <Space>
          <Tooltip title={onlyBound ? '淡化无外部依据的句子，让“有据可溯”更清楚' : '全部句子都会显示；无源句跟随语意保留视觉'}>
            <Switch size="small" checked={onlyBound} onChange={setOnlyBound} checkedChildren="只看证" unCheckedChildren="全文" />
          </Tooltip>
          <Button size="small" icon={<EyeOutlined />} type={refOpen ? 'primary' : 'default'} onClick={() => setRefOpen((o) => !o)}>
            证据密度 · 出处（{boundRows.length}句）
          </Button>
        </Space>
        {noSourceCount > 0 && props.onGoEditNoSource ? (
          <Button size="small" type="link" onClick={props.onGoEditNoSource}>
            有 {noSourceCount} 句待人工核对 → 去编辑处理
          </Button>
        ) : null}
      </div>

      {refOpen ? (
        <Collapse
          ghost
          size="small"
          style={{ marginBottom: 10 }}
          items={boundRows.map((r, i) => ({
            key: i,
            label: (
              <Typography.Text ellipsis style={{ fontSize: 13 }}>
                {r.text}
              </Typography.Text>
            ),
            children: <SourcesCard sources={r.view?.sources ?? []} />,
          }))}
        />
      ) : null}

      <div
        style={{
          border: '1px solid var(--border, #e4e6eb)',
          borderRadius: 8,
          padding: '14px 20px',
          background: 'var(--panel-bg, #fff)',
        }}
      >
        <div style={{ marginBottom: 6, color: 'var(--text-soft,#999)' }}>
          <Space size={4}>
            <BookOutlined />
            <span style={{ fontSize: 13 }}>正文（每句如需溯源，看句尾细标；悬浮 ⓘ 看出处）</span>
          </Space>
        </div>
        <Typography.Paragraph style={{ whiteSpace: 'pre-wrap', margin: 0, lineHeight: 2 }}>
          {rows.map((r) => (
            <span key={`s-${r.view?.sentence_id ?? r.id}`} style={{ opacity: onlyBound && !isBound(r) ? 0.3 : 1, transition: 'opacity .2s' }}>
              {r.text}
              {isBound(r) ? <BoundSourceMarker sources={r.view?.sources ?? []} /> : null}
              {claimOf(r) === 'no_source' ? (
                <NoSourceMark claim="no_source" onHandle={props.onGoEditNoSource ? () => props.onGoEditNoSource?.() : undefined} />
              ) : claimOf(r) === 'human_kept' ? (
                <NoSourceMark claim="human_kept" />
              ) : null}
            </span>
          ))}
        </Typography.Paragraph>
      </div>
      <Typography.Text type="secondary" style={{ display: 'block', marginTop: 6, fontSize: 12 }}>
        视觉约定：蓝 ⓘ＝有据可溯；橙“待核”＝该有据却暂无来源、待你处理；绿“人工保留”＝你确认的自有内容；无标＝纯通稿/衔接。
        改字、增加、删除与“认可/退回待核”请切到「逐句编辑 / 取舍」。
        {props.onGoEditNoSource ? null : ''}
      </Typography.Text>
    </div>
  )
}

function observe(out: Row[], id: number, text: string, viewById: Map<number, SentenceView>) {
  out.push({ id, text, view: viewById.get(id) })
}
