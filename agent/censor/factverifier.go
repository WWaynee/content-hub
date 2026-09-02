// 事实断言校验（闸门二）：撰写完成后，核对稿件每条「数据/事实断言」都能直接回溯到证据原文。
package censor

import (
	"context"
	"fmt"
	"strings"

	"github.com/WWaynee/content-hub/agent"
	"github.com/WWaynee/content-hub/llmclient"
)

// FactVerifier 稿件事实断言校验器。
// 能力边界：允许语义等价/同义改写；禁止规模/统计推断（不得从明细自行求和估算）。
type FactVerifier struct {
	llm llmclient.Client
}

// NewFactVerifier 构造。
func NewFactVerifier(llm llmclient.Client) *FactVerifier {
	return &FactVerifier{llm: llm}
}

// AssertionCheck 稿句中一个数据/事实断言的校验结果。
type AssertionCheck struct {
	// Text 断言的原文表述。
	Text string `json:"text"`
	// Supported 该断言是否能在证据原文中找到直接支撑（语义等价可接受）。
	Supported bool `json:"supported"`
	// EvidenceIdx 支撑它的证据在输入 evidence 数组中的下标（无支撑时为 -1）。
	EvidenceIdx int `json:"evidence_idx"`
}

// SentenceFactCheck 一个稿句的校验结果。
type SentenceFactCheck struct {
	// Index 句子在整篇稿件句子序列中的下标。
	Index int `json:"index"`
	// Text 句子文本。
	Text string `json:"text"`
	// HasDataAssertion 该句是否含数据/事实断言。
	HasDataAssertion bool `json:"has_data_assertion"`
	// Assertions 该句的数据断言明细。
	Assertions []AssertionCheck `json:"assertions"`
}

// FactCheckResult 整篇稿件的校验结果。
type FactCheckResult struct {
	Sentences []SentenceFactCheck
	// Blocked 是否存在「含数据断言但无证据支撑」的句子 → 触发阻断/降级。
	Blocked bool
	// UnsupportedTexts 所有无支撑断言的文本（供报错提示）。
	UnsupportedTexts []string
}

// Check 提取稿件全部数据断言并核对证据覆盖。
// flatSentences：整篇稿件按顺序平铺的句子文本（与 evidence 数组对应）。
func (v *FactVerifier) Check(ctx context.Context, flatSentences []string, evidence []agent.Evidence) (*FactCheckResult, error) {
	if len(flatSentences) == 0 {
		return &FactCheckResult{}, nil
	}
	prompt := buildVerifyPrompt(flatSentences, evidence)
	var out struct {
		Sentences []SentenceFactCheck `json:"sentences"`
	}
	if err := v.llm.ChatWithJSON(ctx, []llmclient.ChatMessage{{Role: "user", Content: prompt}}, &out); err != nil {
		return nil, fmt.Errorf("事实断言校验失败: %w", err)
	}
	res := &FactCheckResult{}
	for _, s := range out.Sentences {
		res.Sentences = append(res.Sentences, s)
		for _, a := range s.Assertions {
			if a.Supported {
				continue
			}
			res.Blocked = true
			res.UnsupportedTexts = append(res.UnsupportedTexts, fmt.Sprintf("句%d：%s", s.Index, a.Text))
		}
	}
	return res, nil
}

func buildVerifyPrompt(sents []string, evidence []agent.Evidence) string {
	var b strings.Builder
	b.WriteString("你是稿件真实性校验助手。系统能力边界：稿件的每一项「事实/数据断言」（具体数字、范围、百分比、日期、条款、对象、事件等有明确指向的事实）必须能直接在下方【检索资料】中找到原文支撑；允许同义词改写和语义等价；**禁止**从多份资料统计求和、估算、推理出原文没有的数字。\n\n")
	b.WriteString("【检索资料】（编号 0 起）\n")
	for i, e := range evidence {
		b.WriteString(fmt.Sprintf("[%d] (文档%d v%s) %s\n", i, e.FileID, e.VersionMd5, e.SourceText))
	}
	b.WriteString("\n【稿件句子】（编号 0 起，逐个校验）\n")
	for i, s := range sents {
		b.WriteString(fmt.Sprintf("[%d] %s\n", i, s))
	}
	b.WriteString(`


【任务】逐句分析每个句子：
1. 是否含数据/事实断言（has_data_assertion）。
2. 对每个断言，判断它能否在【检索资料】某条原文中直接找到支撑（supported），并给出支撑它的证据编号 evidence_idx（无支撑则 -1）。
3. 只返回 JSON，结构：
{"sentences":[{"index":0,"text":"原文","has_data_assertion":true,"assertions":[{"text":"断言","supported":true,"evidence_idx":2}]}]}
- 没有数据断言的句子：has_data_assertion=false，assertions 为空数组。
- 一个句子可含多个断言，逐个标全局证据编号。`)
	return b.String()
}
