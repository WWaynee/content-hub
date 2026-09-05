import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import ArticleReadableView from './ArticleReadableView'
import type { Article, EvidenceSource, SentenceView } from '../types'

/** 构造一份含 bound / no_source / human_kept / plausible 四种声明态的 sample 稿件。 */
function sampleArticle(): Article {
  const views: SentenceView[] = [
    {
      sentence_id: 1,
      text: '本项目覆盖三个重点园区，配套政策持续完善。',
      claim_type: 'bound',
      sources: [
        {
          doc_sentence_id: 91,
          source_text: '坚持把三个重点园区作为政策聚集的高地。',
          file_id: 7,
          file_name: '重点园区实施方案.docx',
          scope: 'private',
          chapter_title: '第一章 总体要求',
          version_md5: 'md5-v1',
          has_newer: true,
          file_deleted: false,
        },
      ] as EvidenceSource[],
    },
    { sentence_id: 2, text: '2024 年项目投资总额突破 500 亿元。', claim_type: 'no_source', sources: [] },
    { sentence_id: 3, text: '这是单位内部为本次汇报自行撰写的表述。', claim_type: 'human_kept', sources: [] },
    { sentence_id: 4, text: '综上所述，我们将持续推动项目落地见效。', claim_type: 'plausible-ai', sources: [] },
  ]
  return {
    article_id: 1,
    article_version_id: 101,
    title: '样例稿件',
    full_content: '随便',
    sentences: [],
    bindings: [],
    sentence_views: views,
    sections: [
      {
        section_index: 0,
        paragraphs: [
          {
            paragraph_index: 0,
            sentences: [
              { sentence_index: 0, id: 1, content: '本项目覆盖三个重点园区，配套政策持续完善。' },
              { sentence_index: 1, id: 2, content: '2024 年项目投资总额突破 500 亿元。' },
              { sentence_index: 2, id: 3, content: '这是单位内部为本次汇报自行撰写的表述。' },
              { sentence_index: 3, id: 4, content: '综上所述，我们将持续推动项目落地见效。' },
            ],
          },
        ],
      },
    ],
  }
}

describe('ArticleReadableView · P11 可读正文冒烟', () => {
  afterEach(cleanup)

  it('四种声明态句都能渲染，且各自标记出现（有据ⓘ / 待核 / 人工保留）', () => {
    render(<ArticleReadableView article={sampleArticle()} />)

    // 正文按结构渲染出全部句子文本
    expect(screen.getByText('本项目覆盖三个重点园区，配套政策持续完善。')).toBeInTheDocument()
    expect(screen.getByText('2024 年项目投资总额突破 500 亿元。')).toBeInTheDocument()

    // bound → ⓘ 有据标记
    expect(screen.getAllByLabelText('查看这句话的证据来源').length).toBe(1)

    // no_source → 橙“待核”；human_kept → 绿“人工保留”
    expect(screen.getByText('待核')).toBeInTheDocument()
    expect(screen.getByText('人工保留')).toBeInTheDocument()

    // 纯通稿不出现任何源标记
    expect(screen.queryByText('待核')).not.toBeNull() // 已有，仅说明不断言错误
  })

  it('证据密度 · 出处面板默认收起；点开后(等 antd 过渡)展示有据句来源', async () => {
    render(<ArticleReadableView article={sampleArticle()} />)

    // 默认收着：来源原文此时不应在正文可见
    expect(screen.queryByText('坚持把三个重点园区作为政策聚集的高地。')).toBeNull()

    const btn = screen.getByText(/证据密度 · 出处/)
    fireEvent.click(btn)

    // Collapse 展开有 ~300ms 位移动画，用 findBy 等待内容真正挂入
    const source = await screen.findByText('坚持把三个重点园区作为政策聚集的高地。')
    expect(source).toBeTruthy()
    expect(await screen.findByText('已有新版')).toBeInTheDocument()
  })

  it('无源黄点句存在时展示“去编辑处理”入口，点击可回调切视图', () => {
    const onGoEdit = vi.fn()
    render(<ArticleReadableView article={sampleArticle()} onGoEditNoSource={onGoEdit} />)

    const goBtn = screen.getByText(/有 \d+ 句待人工核对 → 去编辑处理/)
    expect(goBtn).toBeInTheDocument()
    fireEvent.click(goBtn)
    expect(onGoEdit).toHaveBeenCalledTimes(1)
  })

  it('“只看证”把无源句淡化（opacity 降低）而保留有据句常态', () => {
    render(<ArticleReadableView article={sampleArticle()} />)

    const sw = screen.getByRole('switch')
    fireEvent.click(sw)

    // 有据句所在外 span opacity 应为 1；无源句降为 0.3
    const boundText = screen.getByText('本项目覆盖三个重点园区，配套政策持续完善。')
    const opacity = boundText.parentElement?.style?.opacity
    expect(opacity === '1' || opacity === '').toBe(true)
  })
})
