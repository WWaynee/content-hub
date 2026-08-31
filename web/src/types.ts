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
}

export interface QAMessage {
  id: number
  role: 'user' | 'assistant'
  content: string
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

export interface Article {
  article_id: number
  article_version_id: number
  title: string
  full_content: string
  sentences: ArticleSentence[]
  bindings: EvidenceBinding[]
}

export const PLATFORMS = ['微信公众号', '小红书', '单位网站', '微博']
