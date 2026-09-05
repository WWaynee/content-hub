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
 * readyOnly 仅在 status=draft/needs_req 时才用到：已填可生成 → “可生成”，否则“待填需求”。 */
export function statusAliasLabel(status: string, canGenerate = false): string {
  switch (status) {
    case 'generating':
      return '生成中'
    case 'generated':
      return '已生成'
    case 'revising':
      return '修改中'
    case 'failed':
      return '需重试'
    case 'draft':
    case 'needs_req':
      return canGenerate ? '可生成' : '待填需求'
    default:
      return status || '待填需求'
  }
}
