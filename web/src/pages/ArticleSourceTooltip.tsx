/**
 * ArticleSourceTooltip — P11 的可复用「可溯源」UI 片段。
 *
 * 职责边界：
 *  - SourcesCard：把 P04 装配好的某句 sources 渲染成一块稳定的小卡片（不进不滚、移入停/移出收），
 *    供 tooltip/popover 直接作为 overlay 用；含「复制引文」与 已有新版/已删 语义。
 *  - SentenceMarkers：一句 claim 的「低调 source 标记」+ hover popover 一行封装，
 *    让 bound / plausible / no_source(user_draft) / human_kept 视觉分离、可复用于多种稿面。
 *
 * 本文件只依赖 antd 与 types，不发网络请求——渲染侧纯展示，便于被可读正文视图 / 导出预览复用。
 */
import { useState } from 'react'
import { Button, Popover, Space, Tooltip, Typography, message } from 'antd'
import { CheckCircleOutlined, PaperClipOutlined, SyncOutlined, WarningOutlined } from '@ant-design/icons'
import type { EvidenceSource } from '../types'

/** 复制一段原文到剪贴板（顺带轻提示）。 */
async function copyText(text: string): Promise<void> {
  try {
    await navigator.clipboard.writeText(text)
    message.success('引文已复制')
  } catch {
    message.warning('复制失败，请手动选择复制')
  }
}

const VISIBILITY_LABEL: Record<string, string> = {
  public: '公库',
  private: '私库',
}

/** 单条来源的人读小卡行。 */
function SourceRow({ src }: { src: EvidenceSource }) {
  return (
    <Typography.Paragraph style={{ margin: 0 }}>
      <Typography.Text strong>{src.file_name || `文档 ${src.file_id || '(无)'}`}</Typography.Text>
      {src.scope ? (
        <Typography.Text type="secondary"> · {VISIBILITY_LABEL[src.scope] || src.scope}</Typography.Text>
      ) : null}
      {src.chapter_title ? <Typography.Text type="secondary"> · 章节：{src.chapter_title}</Typography.Text> : null}
      <div style={{ color: 'rgba(0,0,0,0.88)', background: 'rgba(0,0,0,0.03)', borderRadius: 4, padding: '2px 6px', margin: '2px 0' }}>
        {src.source_text}
      </div>
      <Space size="small" wrap>
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          版本 {src.version_md5}
        </Typography.Text>
        {src.has_newer ? (
          <TagText color="#cf1322">已有新版</TagText>
        ) : null}
        {src.file_deleted ? <TagText color="#cf1322">资料已删 · 以上为引用快照</TagText> : null}
        <Button size="small" type="link" style={{ padding: 0 }} onClick={() => copyText(src.source_text)}>
          复制引文
        </Button>
      </Space>
    </Typography.Paragraph>
  )
}

function TagText({ color, children }: { color: string; children: React.ReactNode }) {
  return (
    <span style={{ color, fontSize: 12, fontWeight: 600 }}>{children}</span>
  )
}

/** 一条/多条来源组成的稳定卡片（BoundsSentence hover overlay 用）。 */
export function SourcesCard({ sources }: { sources: EvidenceSource[] }) {
  if (sources.length === 0) {
    return <Typography.Text type="secondary">（没有外部依据）</Typography.Text>
  }
  return (
    <div style={{ maxWidth: 460, maxHeight: 260, overflowY: 'auto', display: 'flex', flexDirection: 'column', gap: 8 }}>
      {sources.map((s, i) => (
        <SourceRow key={`${s.doc_sentence_id}-${i}`} src={s} />
      ))}
      {sources.length > 1 ? (
        <Space>
          <Button size="small" icon={<PaperClipOutlined />} onClick={() => copyText(sources.map((s) => s.source_text).join('\n'))}>
            复制全部引文（{sources.length}）
          </Button>
        </Space>
      ) : null}
    </div>
  )
}

/** 有界句内标记(tile)：低调、有源；hover 悬浮来源卡片。place 放于 popover 内。 */
export function BoundSourceMarker({ sources }: { sources: EvidenceSource[] }) {
  const [open, setOpen] = useState(false)
  return (
    <Popover
      trigger={['hover', 'focus']}
      open={open}
      onOpenChange={setOpen}
      placement="top"
      title={
        <Space>
          <span>这句话的出处</span>
          <Button size="small" type="link" style={{ fontSize: 12, padding: 0 }} onClick={() => copyText(sources[0]?.source_text || '')}>
            复制引文
          </Button>
        </Space>
      }
      content={<SourcesCard sources={sources} />}
    >
      <span tabIndex={0} aria-label="查看这句话的证据来源" style={{ cursor: 'pointer', color: '#1677ff', margin: '0 2px', fontSize: 12, userSelect: 'none' }}>
        ⓘ
      </span>
    </Popover>
  )
}

/** 无源两类(no_source 待核 / human_kept 作者认可)的黄点式标记与 human 保留标记。 */
export function NoSourceMark({ claim, onHandle }: { claim: string; onHandle?: () => void }) {
  const isNoSource = claim === 'no_source'
  const isKept = claim === 'human_kept'
  if (!isNoSource && !isKept) {
    return null
  }
  const color = isNoSource ? '#faad14' : '#52c41a'
  const text = isNoSource ? '待核' : '人工保留'
  return (
    <Tooltip
      title={
        isNoSource
          ? '这句是你稿中带数据/事实、却暂无知识库可引依据的黄点句。请处理：认可保留 / 补资料后更正 / 删除。'
          : '这句已被作者确认为其自有内容（无外部依据、不再提示）。'
      }
    >
      <span
        // 留意：保留为宽松标签
        onClick={onHandle}
        style={{
          display: 'inline-flex',
          alignItems: 'center',
          gap: 4,
          color,
          fontSize: 12,
          fontWeight: 600,
          cursor: onHandle && isNoSource ? 'pointer' : 'default',
          margin: '0 4px',
          userSelect: 'none',
        }}
      >
        {isNoSource ? <WarningOutlined /> : isKept ? <CheckCircleOutlined /> : <SyncOutlined />}
        {text}
      </span>
    </Tooltip>
  )
}
