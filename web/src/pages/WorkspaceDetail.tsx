import { useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import api from '../api'
import type { Requirement, Article } from '../types'
import { PLATFORMS } from '../types'

export default function WorkspaceDetail() {
  const { id } = useParams()
  const wid = Number(id)
  const [phase, setPhase] = useState<'requirement' | 'article'>('requirement')

  const [req, setReq] = useState<Requirement | null>(null)
  const [article, setArticle] = useState<Article | null>(null)
  const [generating, setGenerating] = useState(false)
  const [exported, setExported] = useState('')

  // 对话
  const [chat, setChat] = useState('')

  const loadReq = async () => {
    const r = (await api.get(`/workspaces/${wid}/requirement`)) as any
    setReq(r)
  }
  const loadArticle = async () => {
    try {
      const a = (await api.get(`/workspaces/${wid}/article`)) as any
      setArticle(a)
    } catch {
      setArticle(null)
    }
  }

  useEffect(() => {
    loadReq()
    loadArticle()
  }, [wid])

  const saveReq = async () => {
    if (!req) return
    await api.put(`/requirements/${req.id}`, req)
    alert('已保存')
    loadReq()
  }

  const generate = async () => {
    setGenerating(true)
    try {
      await api.post(`/workspaces/${wid}/generate`)
      await loadArticle()
      setPhase('article')
    } catch (e: any) {
      alert('生成失败: ' + (e.message || ''))
    } finally {
      setGenerating(false)
    }
  }

  const doExport = async () => {
    if (!article) return
    const r = (await api.get(`/articles/${article.article_version_id}/export`)) as any
    setExported(r.markdown)
  }

  const sendChat = async () => {
    if (!chat || !req) return
    // 对话接口一期简化：直接改需求单风格字段（用表单演示）；完整 DialoguePlan 对话见后端阶段7C延伸
    alert('对话功能后端接口待接入（DialoguePlan 派发），当前请用表单手动修改需求单')
    setChat('')
  }

  const setReqField = (k: keyof Requirement, v: any) => {
    setReq((r) => (r ? { ...r, [k]: v } : r))
  }

  return (
    <div style={{ display: 'flex', height: '100%' }}>
      {/* 左侧阶段切换 */}
      <div style={{ width: 120, borderRight: '1px solid #eee' }}>
        <button onClick={() => setPhase('requirement')} disabled={phase === 'requirement'}>需求</button>
        <button onClick={() => setPhase('article')} disabled={phase === 'article'}>稿件</button>
      </div>

      {/* 主内容 */}
      <div style={{ flex: 1, padding: '0 16px' }}>
        {phase === 'requirement' && req && (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 8, maxWidth: 600 }}>
            <label>标题</label>
            <input value={req.title} onChange={(e) => setReqField('title', e.target.value)} />
            <label>标签（逗号分隔）</label>
            <input value={(req.tags || []).join(',')} onChange={(e) => setReqField('tags', e.target.value.split(','))} />
            <label>发布平台</label>
            <select multiple value={req.platforms || []} onChange={(e) => setReqField('platforms', Array.from(e.target.selectedOptions, (o) => o.value))}>
              {PLATFORMS.map((p) => <option key={p} value={p}>{p}</option>)}
            </select>
            <label>基调</label>
            <input value={req.style_tone} onChange={(e) => setReqField('style_tone', e.target.value)} />
            <label>感情色彩</label>
            <input value={req.style_emotion} onChange={(e) => setReqField('style_emotion', e.target.value)} />
            <label>目标受众</label>
            <input value={req.style_audience} onChange={(e) => setReqField('style_audience', e.target.value)} />
            <label>发文目的</label>
            <input value={req.style_purpose} onChange={(e) => setReqField('style_purpose', e.target.value)} />
            <label>发文主体</label>
            <input value={req.style_subject} onChange={(e) => setReqField('style_subject', e.target.value)} />
            <label>禁忌/约束</label>
            <textarea value={req.style_taboo} onChange={(e) => setReqField('style_taboo', e.target.value)} />
            <label>字数要求</label>
            <input type="number" value={req.word_count} onChange={(e) => setReqField('word_count', Number(e.target.value))} />
            <label>章节要求</label>
            <textarea value={req.chapter_requirement} onChange={(e) => setReqField('chapter_requirement', e.target.value)} />

            <div>
              <button onClick={saveReq}>保存需求单</button>
              <button onClick={generate} disabled={generating} style={{ marginLeft: 8 }}>
                {generating ? '生成中...' : '生成稿件'}
              </button>
            </div>
          </div>
        )}

        {phase === 'article' && (
          <div>
            {article ? (
              <div>
                <h2>{article.title}</h2>
                <button onClick={doExport}>导出</button>
                {exported && (
                  <pre style={{ whiteSpace: 'pre-wrap', border: '1px solid #eee', padding: 12, background: '#fafafa' }}>{exported}</pre>
                )}
                <h3>正文</h3>
                <div style={{ whiteSpace: 'pre-wrap' }}>{article.full_content}</div>
                <h3>句子（含证据）</h3>
                {article.sentences.map((s) => {
                  const bindings = article.bindings.filter((b) => b.article_sentence_id === s.id)
                  return (
                    <div key={s.id}>
                      {s.content}
                      {bindings.length > 0 && <span style={{ color: '#2563eb' }}> ← 证据 x{bindings.length}</span>}
                    </div>
                  )
                })}
              </div>
            ) : (
              <p>尚未生成稿件，请先在需求阶段点击「生成稿件」</p>
            )}
          </div>
        )}
      </div>

      {/* 右侧对话 */}
      <div style={{ width: 280, borderLeft: '1px solid #eee', padding: 8 }}>
        <strong>对话</strong>
        <textarea style={{ width: '100%', height: 160 }} value={chat} onChange={(e) => setChat(e.target.value)} />
        <button onClick={sendChat} style={{ width: '100%' }}>发送</button>
      </div>
    </div>
  )
}
