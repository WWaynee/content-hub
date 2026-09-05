// P12（W4/W2）前端纯逻辑：可生成硬前置、缺项人话、状态别名。
// 独立成纯函数便于 vitest 冒烟（不依赖 React/网络）。

/** 判别所需的最小需求单形状（前端既可基于详情 Requirement，也类用在卡片展示的衍生）。
 * platforms 为数组（详情）；走卡片时若只有字符串逗号串，可先 split 成数组。 */
export interface RequirementLike {
  title?: string
  platforms?: string[] | null
  style_tone?: string
  style_emotion?: string
  style_audience?: string
  style_purpose?: string
  style_taboo?: string
  style_subject?: string
  word_count?: number
  chapter_requirement?: string
}

export function splitPlatforms(raw?: string): string[] {
  if (!raw) return []
  return raw
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean)
}

function anySpec(r: RequirementLike): boolean {
  return !!(
    r.style_tone?.trim() ||
    r.style_emotion?.trim() ||
    r.style_audience?.trim() ||
    r.style_purpose?.trim() ||
    r.style_taboo?.trim() ||
    r.style_subject?.trim() ||
    r.chapter_requirement?.trim() ||
    (r.word_count ?? 0) > 0
  )
}

/** 需求单“还缺什么”的人话清单（与后端 RequirementCompletenessIssues 口径一致引用范围允许空=全部）。 */
export function requirementMissing(req: RequirementLike | null | undefined): string[] {
  if (!req) return ['需求单数据暂缺']
  const missing: string[] = []
  if (!req.title?.trim()) missing.push('标题')
  if (!req.platforms || req.platforms.length === 0) missing.push('发布平台')
  if (!anySpec(req)) missing.push('发文风格 / 字数 / 章节要求（至少其一）')
  return missing
}

export function isRequirementReady(req: RequirementLike | null | undefined): boolean {
  return requirementMissing(req).length === 0
}

/** 工作区状态的“工话别名”，收敛内部 draft/needs_req 两个近义为“待填需求”。
 * 仅 status=draft/needs_req 时 canGenerate 参与：已填可生成 → “可生成”，否则“待填需求”。
 * 文案与列表筛选/卡片共用一个来源（STATUS_META 亦由此派生），避免两套不一致。 */
export function statusAliasLabel(status: string, canGenerate = false): string {
  switch (status) {
    case 'generating':
      return '生成中'
    case 'generated':
      return '已生成'
    case 'revising':
      return '修改中'
    case 'failed':
      return '生成失败'
    case 'draft':
    case 'needs_req':
      return canGenerate ? '可生成' : '待填需求'
    default:
      return status || '待填需求'
  }
}

/** 列表卡片：从“工作区+需求单摘要”字段构造需求判别形状（platforms 为 JSON 文本时解析成数组）。 */
export interface WorkspaceCard {
  status?: string
  requirement_title?: string
  requirement_platforms?: string | null
  requirement_word_count?: number
  requirement_style_tone?: string
  requirement_style_emotion?: string
  requirement_style_audience?: string
  requirement_style_purpose?: string
  requirement_style_subject?: string
  requirement_chapter_requirement?: string
}

/** 把列表卡片字段解析成 RequirementLike；platforms 兼容 JSON 数组文本/逗号串/空。 */
export function requirementLikeFromCard(w: WorkspaceCard): RequirementLike {
  let platforms: string[] | null = null
  const raw = w?.requirement_platforms
  if (raw) {
    try {
      const parsed = JSON.parse(raw)
      if (Array.isArray(parsed)) platforms = parsed.map((x: unknown) => String(x))
    } catch {
      platforms = splitPlatforms(raw)
    }
  }
  return {
    title: w?.requirement_title ?? '',
    platforms: platforms ?? [],
    style_tone: w?.requirement_style_tone ?? '',
    style_emotion: w?.requirement_style_emotion ?? '',
    style_audience: w?.requirement_style_audience ?? '',
    style_purpose: w?.requirement_style_purpose ?? '',
    style_subject: w?.requirement_style_subject ?? '',
    word_count: w?.requirement_word_count ?? 0,
    chapter_requirement: w?.requirement_chapter_requirement ?? '',
  }
}

/** 列表卡片状态别名（带“可生成/待填需求”派生）：draft 且需求已齐 → 可生成。 */
export function cardStatusLabel(w: WorkspaceCard | null | undefined): string {
  if (!w) return '待填需求'
  return statusAliasLabel(w.status ?? '', isRequirementReady(requirementLikeFromCard(w)))
}
