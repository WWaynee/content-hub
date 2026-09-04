/**
 * ArticleSentencesBoard — P09 的"人在稿面上直接动手"面板。
 *
 * - 按 sentence 列表(结构性升序)逐句展示：文本、该句声明态(claim_type)、操作；
 * - 每句可「改这句 / 这句后插一句 / 删这句」→ 组成 P08 的单个 change_list op 提交到
 *   PATCH sequence（后端跑 P09：新增未附来源的句子会自动落 no_source 占位，见请求返回 reviews）；
 * - 对 no_source/human_kept 句提供作者取舍：ack(认可无外部依据，解除黄点)、删除；
 *   UI 一律给人话结果而非透传 tool 名。
 */
import { useMemo, useState } from 'react'
import { Button, Input, Popconfirm, Space, Tag, Tooltip, Typography, message } from 'antd'
import {
  DeleteOutlined,
  EditOutlined,
  PlusOutlined,
  ReloadOutlined,
  CheckCircleOutlined,
  QuestionCircleOutlined,
} from '@ant-design/icons'
import api from '../api'
import type { Article } from '../types'

interface Row {
  id: number
  text: string
  claim?: string
  srcCount: number
}

function MarkOf(claim?: string): { label: string; color: string; tip: string } | null {
  switch (claim) {
    case 'bound':
      return { label: '有据可溯', color: 'blue', tip: '该句可在知识库中找到可引用来源。' }
    case 'no_source':
      return { label: '无外部·待核', color: 'orange', tip: '这句疑似该有据却未能找到外部来源，正文已保留，请你决定：认可保留 / 补资料 / 删除。' }
    case 'human_kept':
      return { label: '人工保留', color: 'green', tip: '该句已被作者认可为其自有内容，无外部依据但不打扰。' }
    case 'plausible-ai':
    default:
      return { label: '纯通稿', color: 'default', tip: '语义衔接语或无外部引用的通稿句。' }
  }
}

export default function ArticleSentencesBoard({ wid, article, onCommitted }: {
  wid: number
  article: Article
  onCommitted: () => void | Promise<void>
}) {
  const [busy, setBusy] = useState(false)
  const [editingId, setEditingId] = useState<number | 0>(0)
  const [draft, setDraft] = useState('')
  const [insertAfterId, setInsertAfterId] = useState<number | 0>(0)
  const [insertText, setInsertText] = useState('')
  // 移动两步式：moveFrom>0 = 已选定要搬的那句；再点另一句的「移到这里」→ 落到其后。
  const [moveFrom, setMoveFrom] = useState<number | 0>(0)

  const rows: Row[] = useMemo(() => {
    // 从 props 派生（edit/insert/delete 后 reload 由父组件拉新 article，让本组件无本地脏样本）
    const byViewId = new Map<number, { claim?: string; srcCount: number }>()
    for (const v of article.sentence_views ?? []) {
      byViewId.set(v.sentence_id, { claim: v.claim_type, srcCount: v.sources.length })
    }
    return (article.sentences ?? []).map((s) => {
      const info = byViewId.get(s.id)
      return {
        id: s.id,
        text: s.content,
        claim: info?.claim,
        srcCount: info?.srcCount ?? 0,
      }
    })
  }, [article])

  const bump = async (run: () => Promise<void>) => {
    setBusy(true)
    try {
      await run()
      await onCommitted()
    } catch (e: any) {
      message.error(e?.message || '操作失败')
    } finally {
      setBusy(false)
    }
  }

  const saveEdit = async (rowId: number) => {
    if (!draft.trim()) return
    await bump(async () => {
      const r: any = await api.patch(`/workspaces/${wid}/article/sequence`, {
        govern: true, // P09：服务端对"带不出证的改动句"跑一次真治理(bound/no_source/plausible)再落库
        ops: [{ op: 'edit', target_sentence_id: rowId, new_text: draft.trim() }],
      })
      message.info(r?.human_text || '已把改动保存')
      setEditingId(0)
      setDraft('')
    })
  }

  const insertAfter = async (anchorId: number) => {
    const t = insertText.trim()
    if (!t) return
    await bump(async () => {
      const r: any = await api.patch(`/workspaces/${wid}/article/sequence`, {
        govern: true, // 同上：让服务端对这次引入的新句做一次真治理
        ops: [{ op: 'insert', anchor_sentence_id: anchorId, new_text: t }],
      })
      message.info(r?.human_text || '已插入一句')
      setInsertAfterId(0)
      setInsertText('')
    })
  }

  const deleteSentence = async (rowId: number) => {
    await bump(async () => {
      const r: any = await api.patch(`/workspaces/${wid}/article/sequence`, {
        ops: [{ op: 'delete', target_sentence_id: rowId }],
      })
      message.info(r?.human_text || '已删除这句')
    })
  }

  const moveInvoke = async (targetId: number, anchorId: number) => {
    await bump(async () => {
      const r: any = await api.patch(`/workspaces/${wid}/article/sequence`, {
        ops: [{ op: 'move', target_sentence_id: targetId, anchor_sentence_id: anchorId }],
      })
      message.info(r?.human_text || '已把这句挪到目标句之后')
      setMoveFrom(0)
    })
  }

  const bumpMark = async (rowId: number, human: boolean) => {
    await bump(async () => {
      const r: any = await api.patch(`/workspaces/${wid}/article/mark`, {
        sentence_id: rowId,
        action: human ? 'ack_human' : 'reset_no_source',
      })
      message.success(r?.meta?.message || (human ? '已认可保留（不再黄点提醒）' : '已退回待核'))
    })
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      {rows.length === 0 ? (
        <Typography.Text type="secondary">这篇稿子尚无句子；先在需求单生成，或用界面给某句后面添加新句。</Typography.Text>
      ) : null}
      {rows.map((row, idx) => {
        const seqNo = idx + 1
        const mark = MarkOf(row.claim)
        const isEdit = editingId === row.id
        const showInsert = insertAfterId === row.id
        return (
          <div key={row.id} style={{ border: '1px solid var(--border)', borderRadius: 8, padding: '8px 10px' }}>
            {moveFrom > 0 && row.id !== moveFrom && !busy ? (
              <Button size="small" type="dashed" style={{ marginBottom: 6 }} onClick={() => moveInvoke(moveFrom, row.id)}>
                ⇣ 放到本句之后
              </Button>
            ) : null}
            {mark ? (
              <div style={{ marginBottom: 6 }}>
                <Tag color={mark.color}>{mark.label}</Tag>
                <Tooltip title={mark.tip}>
                  <QuestionCircleOutlined style={{ color: mark.color === 'orange' ? '#faad14' : '#888', marginLeft: 4 }} />
                </Tooltip>
                <span style={{ marginLeft: 8, opacity: 0.7 }}>#{seqNo}</span>
                <Button
                  size="small"
                  type={row.id === moveFrom ? 'link' : 'text'}
                  style={{ marginLeft: 8 }}
                  disabled={busy}
                  onClick={() => { setMoveFrom(row.id === moveFrom ? 0 : row.id) }}
                >
                  {row.id === moveFrom ? '已选·点别句「放到本句之后」' : '把它移到别句后'}
                </Button>
              </div>
            ) : null}

            {isEdit ? (
              <Input.TextArea rows={2} value={draft} autoFocus onChange={(e) => setDraft(e.target.value)} />
            ) : (
              <Typography.Paragraph style={{ marginBottom: 4, whiteSpace: 'pre-wrap' }}>{row.text}</Typography.Paragraph>
            )}

            <Space size={4} wrap style={{ marginTop: 4 }}>
              {isEdit ? (
                <Button size="small" type="primary" disabled={busy} onClick={() => saveEdit(row.id)}>
                  保存这句
                </Button>
              ) : (
                <Button
                  size="small"
                  icon={<EditOutlined />}
                  disabled={busy}
                  onClick={() => {
                    setEditingId(row.id)
                    setDraft(row.text)
                  }}
                >
                  改这句
                </Button>
              )}
              {isEdit && (
                <Button size="small" onClick={() => { setEditingId(0); setDraft('') }}>取消</Button>
              )}

              <Button
                size="small"
                icon={<PlusOutlined />}
                disabled={busy}
                onClick={() => {
                  setInsertAfterId(showInsert ? 0 : row.id)
                  setInsertText('')
                }}
              >
                句后+插
              </Button>

              <Popconfirm title="删除这一句及其来源标注？" onConfirm={() => deleteSentence(row.id)} disabled={busy}>
                <Button size="small" icon={<DeleteOutlined />} danger disabled={busy}>删</Button>
              </Popconfirm>

              {(row.claim === 'no_source' || row.claim === 'human_kept') && (
                row.claim === 'no_source' ? (
                  <Button size="small" icon={<CheckCircleOutlined />} onClick={() => bumpMark(row.id, true)}>
                    认可保留（不再黄）
                  </Button>
                ) : (
                  <Button size="small" icon={<ReloadOutlined />} onClick={() => bumpMark(row.id, false)}>
                    退回待核
                  </Button>
                )
              )}
            </Space>

            {showInsert && (
              <div style={{ marginTop: 8 }}>
                <Input
                  placeholder="输入要在这句之后加入的一句文本"
                  value={insertText}
                  onChange={(e) => setInsertText(e.target.value)}
                  onPressEnter={() => insertAfter(row.id)}
                />
                <Button type="primary" size="small" disabled={busy || !insertText.trim()} onClick={() => insertAfter(row.id)} style={{ marginTop: 6 }}>
                  确认插入
                </Button>
              </div>
            )}
          </div>
        )
      })}
    </div>
  )
}
export { MarkOf }
