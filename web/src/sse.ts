// SSE 进度接收（P13）。项目鉴权走 Authorization 头，浏览器原生 EventSource 无法带 header，
// 因此用 fetch + ReadableStream 自己解析 `event: …/data: …` 流，效果等同 SSE（真正的增量推送）。
export type GenEv =
  | { type: 'step_begin'; stepNo: number }
  | { type: 'step_detail'; stepNo: number; detail?: string }
  | { type: 'step_done'; stepNo: number; detail?: string; durationMs?: number }
  | { type: 'step_fail'; stepNo: number; failure?: string }
  | { type: 'run_done'; payload?: any }
  | { type: 'run_failed'; payload?: any }

// openGenerateStream 用 fetch 长连读取生成进度流，逐 event 回调。返回用于手动中断的 abort。
export function openGenerateStream(
  wid: number,
  runId: number,
  onEvent: (ev: GenEv) => void,
  onError?: (err: string) => void,
): () => void {
  const ctrl = new AbortController()
  const token = localStorage.getItem('token') || ''
  // eslint-disable-next-line no-async-promise-executor
  ;(async () => {
    try {
      const resp = await fetch(`/api/workspaces/${wid}/generate/stream?run_id=${runId}`, {
        headers: { Authorization: `Bearer ${token}` },
        signal: ctrl.signal,
      })
      if (!resp.ok || !resp.body) {
        onError?.(`进度连接失败 HTTP ${resp.status}`)
        return
      }
      const reader = resp.body.getReader()
      const decoder = new TextDecoder()
      let buf = ''
      for (;;) {
        const { done, value } = await reader.read()
        if (done) break
        buf += decoder.decode(value, { stream: true })
        // SSE: 每块以空行分隔
        let idx: number
        while ((idx = buf.indexOf('\n\n')) >= 0) {
          const block = buf.slice(0, idx)
          buf = buf.slice(idx + 2)
          const ev = parseBlock(block)
          if (ev) onEvent(ev)
        }
      }
    } catch (e: any) {
      if (e?.name !== 'AbortError') onError?.(e?.message || '进度连接中断')
    }
  })()

  return () => ctrl.abort()
}

function parseBlock(block: string): GenEv | null {
  let type = ''
  const datas: string[] = []
  for (const line of block.split('\n')) {
    if (line.startsWith('event:')) type = line.slice('event:'.length).trim()
    else if (line.startsWith('data:')) datas.push(line.slice('data:'.length).trim())
  }
  if (!type || datas.length === 0) return null
  let obj: any = {}
  try {
    obj = JSON.parse(datas.join('\n'))
  } catch {
    return null
  }
  const stepNo = Number(obj?.step_no || obj?.StepNo || 0)
  const pay = obj?.payload || {}
  const detail = typeof pay === 'string' ? pay : pay?.detail || ''
  const failure = (typeof pay === 'string' ? '' : pay?.failure) || ''
  switch (type) {
    case 'step_begin':
      return { type: 'step_begin', stepNo }
    case 'step_detail':
      return { type: 'step_detail', stepNo, detail: String(detail) }
    case 'step_done':
      return { type: 'step_done', stepNo, detail: String(detail), durationMs: pay?.duration_ms || pay?.DurationMs || 0 }
    case 'step_fail':
      return { type: 'step_fail', stepNo, failure: String(failure) || '该步骤未通过' }
    case 'run_done':
      return { type: 'run_done', payload: pay }
    case 'run_failed':
      return { type: 'run_failed', payload: typeof pay === 'string' ? pay : obj?.payload }
    default:
      return null
  }
}
