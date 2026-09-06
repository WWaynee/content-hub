package retrieve

import (
	"context"
	"fmt"
	"strings"

	"github.com/WWaynee/content-hub/agent/guardian"
	"github.com/WWaynee/content-hub/llmclient"
)

// P14 §2.3：LLM 查询扩展（Query Expansion）。
//
// 现状：Guardian 的 ThinkFn 是 stagedThinker（规则式：第 1 轮原 query，之后机械加
// "、"/"规定 要求 "前缀换词），表述多样性有限——某些 claim 换一种说法才能召回。
// 升级：用 LLM 一次把 claim 扩展为 count 条不同表述的 query（关键词直述/同义改写/
// 口语化/正式化……），ThinkFn 逐条吐出；Guardian 的多轮循环天然完成「多 query 检索
// + doc_sentence 去重合并」，相当于第一轮检索质量更高。
//
// 降级原则：LLM 扩展失败/产出为空时退回「仅原 claim」——绝不因扩展层故障让检索无 query。

// llmQueryExpander 带缓存的 LLM 查询扩展器（实现 guardian.ThinkFn 语义）。
type llmQueryExpander struct {
	llm      llmclient.Client
	count    int
	expanded []string // 一次 LLM 调用产出的全部候选 query（含去重清理）
	next     int      // 下一条要吐出的下标
}

// expand 调用 LLM 生成 count 条不同表述；失败或空结果时保底为仅原 claim。
func (e *llmQueryExpander) expand(ctx context.Context, claim string) {
	if e.count <= 1 {
		e.count = 3
	}
	prompt := fmt.Sprintf(`你是政企内容检索助手。下面是一个需要从知识库检索证据支撑的子需求点。
请用 %d 种不同表述写出等价的检索词/问题：覆盖「关键词直述」「同义改写」「更口语/更正式的说法」等角度，
不同写法要能召回不同片段（专有名词/编号尽量保留）。只返回 JSON：{"queries":["...","..."]}，共 %d 条，不要解释。

子需求点：%s`, e.count, e.count, claim)

	var resp struct {
		Queries []string `json:"queries"`
	}
	if err := e.llm.ChatWithJSON(ctx, []llmclient.ChatMessage{{Role: "user", Content: prompt}}, &resp); err != nil {
		e.expanded = []string{claim} // LLM 故障：降级保底
		return
	}
	seen := map[string]bool{}
	for _, q := range resp.Queries {
		q = strings.TrimSpace(q)
		if q == "" || seen[q] {
			continue
		}
		seen[q] = true
		e.expanded = append(e.expanded, q)
	}
	if len(e.expanded) == 0 {
		e.expanded = []string{claim}
	}
}

// call 实现 guardian.ThinkFn：逐条给出尚未试过的扩展 query；全部试过返回 false。
func (e *llmQueryExpander) call(ctx context.Context, claim string, tried []string) (string, bool, error) {
	if e.expanded == nil {
		e.expand(ctx, claim)
	}
	triedSet := map[string]bool{}
	for _, t := range tried {
		triedSet[t] = true
	}
	for e.next < len(e.expanded) {
		q := e.expanded[e.next]
		e.next++
		if !triedSet[q] {
			return q, true, nil
		}
	}
	return "", false, nil
}

// NewLLMQueryThinkFn 返回基于 LLM 查询扩展的 Guardian ThinkFn（P14 可选升级）。
// count<=1 时按 3 处理；任何失败路径都降级为「仅原 claim」，不硬抛。
func NewLLMQueryThinkFn(llm llmclient.Client, count int) guardian.ThinkFn {
	e := &llmQueryExpander{llm: llm, count: count}
	return e.call
}

// StagedThinkFn 规则式多轮换词 Thinker（P06 默认产线实现，P14 后仍是默认）。
func StagedThinkFn() guardian.ThinkFn {
	return (&stagedThinker{}).call
}

// ExpandQuery 一次把 query 扩展为 count 条不同表述（供评测基准等需要「多 query 并行检索 +
// 合并去重」的调用方直接使用）；失败/空结果时保底返回 [query]，绝不返回空集。
func ExpandQuery(ctx context.Context, llm llmclient.Client, query string, count int) []string {
	e := &llmQueryExpander{llm: llm, count: count}
	e.expand(ctx, query)
	return e.expanded
}
