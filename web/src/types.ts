export interface User {
  id: number
  tenant_id: number
  username: string
  role: 'admin' | 'member'
}

export interface Workspace {
  id: number
  tenant_id: number
  owner_user_id: number
  title: string
  status: string
  created_at: string
  updated_at: string
  requirement_title?: string
  requirement_tags?: string
  requirement_platforms?: string
  requirement_word_count?: number
  requirement_style_tone?: string
  requirement_style_emotion?: string
  requirement_style_audience?: string
  requirement_style_purpose?: string
  requirement_style_subject?: string
  requirement_chapter_requirement?: string
  requirement_version?: number
}

export interface Requirement {
  id: number
  workspace_id: number
  tenant_id: number
  title: string
  tags: string[]
  platforms: string[]
  style_tone: string
  style_emotion: string
  style_audience: string
  style_purpose: string
  style_taboo: string
  style_subject: string
  word_count: number
  chapter_requirement: string
  version: number
  /** P10：起稿方式；draft_assist=粘贴用户自带草稿起稿，默认 build_from_scratch。 */
  source_kind?: 'build_from_scratch' | 'draft_assist' | string
  /** P10：draft_assist 模式保存的用户草稿原文。 */
  draft_input?: string
}

export interface KbaseDir {
  id: number
  name: string
  scope: string
  parent_id: number
  created_at: string
  updated_at: string
}

export interface KbaseFile {
  id: number
  name: string
  scope: string
  dir_id: number
  file_type: string
  size: number
  current_version_md5: string
  created_at: string
  updated_at: string
}

export interface QASession {
  id: number
  title: string
  created_at: string
  updated_at: string
  /** 仅前端用：是否为「未创建」的草稿新会话（尚未提问，未落库）。 */
  temp?: boolean
}

export interface QAMessage {
  id: number
  role: 'user' | 'assistant'
  content: string
  /** 仅前端用：是否为"思考中"占位消息（回答尚未返回）。 */
  pending?: boolean
}

export interface ArticleSentence {
  id: number
  sentence_index: number
  content: string
}

export interface EvidenceBinding {
  id: number
  article_sentence_id: number
  doc_file_id: number
  doc_sentence_id: number
}

/** P04 证据"人读 source"：EvidenceBinding 展开后的可读引用来源（RFC rev-2 §8.2-Q2 / rev-4 W6）。 */
export interface EvidenceSource {
  doc_sentence_id: number
  source_text: string
  file_id: number
  file_name: string
  scope?: string
  chapter_title?: string
  version_md5: string
  /** 引用之后资料又更新过新版本（当前文件 current_version_md5 ≠ 引用版本）。 */
  has_newer: boolean
  /** 来源文档当前已被删除（active=0），此处为引用时保留的原文快照。 */
  file_deleted: boolean
}

/** P04 稿件某一句的人读视图（RFC rev-2 §10.1）：claim_type + 可读 sources。 */
export interface SentenceView {
  sentence_id: number
  text: string
  /** bound：有据可溯源；plausible-ai：纯 AI 通稿，无外部引用。 */
  claim_type: 'bound' | 'plausible-ai' | string
  sources: EvidenceSource[]
}

export interface Article {
  article_id: number
  article_version_id: number
  title: string
  full_content: string
  sentences: ArticleSentence[]
  bindings: EvidenceBinding[]
  sentence_views?: SentenceView[]
}

export const PLATFORMS = ['微信公众号', '小红书', '单位网站', '微博']
