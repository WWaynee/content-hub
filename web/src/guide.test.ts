import { describe, expect, it } from 'vitest'
import {
  isRequirementReady,
  requirementMissing,
  splitPlatforms,
  statusAliasLabel,
  requirementLikeFromCard,
  cardStatusLabel,
} from './guide'

function anyContains(items: string[], sub: string): boolean {
  return items.some((x) => x.includes(sub))
}

describe('P12 · guide 纯函数单测', () => {
  it('缺项->可生成 判定与后端口径一致', () => {
    expect(isRequirementReady({ title: '招生稿', platforms: ['官网'], style_tone: '严谨' })).toBe(true)
    expect(anyContains(requirementMissing({ title: '招生稿', platforms: [] }), '发布平台')).toBe(true)
    expect(anyContains(requirementMissing({ title: '招生稿', platforms: ['官网'] }), '发文风格')).toBe(true)
    expect(anyContains(requirementMissing({ title: '', platforms: ['官网'], word_count: 800 }), '标题')).toBe(true)
    expect(anyContains(requirementMissing({ title: '  ', platforms: ['官网'], word_count: 800 }), '标题')).toBe(true)
    expect(isRequirementReady(null)).toBe(false)
    expect(isRequirementReady(undefined)).toBe(false)
  })

  it('字数/章节也算“有规格”', () => {
    expect(isRequirementReady({ title: 'x', platforms: ['官网'], word_count: 300 })).toBe(true)
    expect(isRequirementReady({ title: 'x', platforms: ['官网'], chapter_requirement: '含报名' })).toBe(true)
  })

  it('splitPlatforms 兼容逗号/空白串', () => {
    expect(splitPlatforms('官网, 公众号')).toEqual(['官网', '公众号'])
    expect(splitPlatforms('')).toEqual([])
    expect(splitPlatforms(undefined)).toEqual([])
  })

  it('status 别名收敛 draft/needs_req 两近义，且合并为“待填/可生成”', () => {
    expect(statusAliasLabel('draft', false)).toBe('待填需求')
    expect(statusAliasLabel('needs_req', false)).toBe('待填需求')
    expect(statusAliasLabel('draft', true)).toBe('可生成')
    expect(statusAliasLabel('generated')).toBe('已生成')
    expect(statusAliasLabel('generating')).toBe('生成中')
    expect(statusAliasLabel('failed')).toBe('生成失败')
  })

  it('卡片需求解析：platforms 兼容 JSON 数组文本/逗号串，可生成派生供状态标签', () => {
    // 后端列表返回 platforms 是 JSON 数组文本
    expect(requirementLikeFromCard({ requirement_platforms: '["官网","公众号"]' }).platforms).toEqual(['官网', '公众号'])
    // 逗号串兜底
    expect(requirementLikeFromCard({ requirement_platforms: '官网,公众号' }).platforms).toEqual(['官网', '公众号'])
    // 空/null → []
    expect(requirementLikeFromCard({}).platforms).toEqual([])
    // draft + 需求齐 → 可生成；需求缺平台 → 待填需求
    expect(
      cardStatusLabel({
        status: 'draft',
        requirement_title: '招生稿',
        requirement_platforms: '["官网"]',
        requirement_style_tone: '严谨',
      }),
    ).toBe('可生成')
    expect(cardStatusLabel({ status: 'draft', requirement_title: '招生稿', requirement_platforms: '[]' })).toBe('待填需求')
    // 非 draft 状态不参与派生
    expect(cardStatusLabel({ status: 'generated' })).toBe('已生成')
    expect(cardStatusLabel(undefined)).toBe('待填需求')
  })
})
