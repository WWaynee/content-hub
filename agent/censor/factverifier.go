// 事实断言校验（闸门二）：撰写完成后，核对稿件每条「数据/事实断言」都能直接回溯到证据原文。
package censor

import (
	"context"
	"fmt"
	"strings"

	"github.com/WWaynee/content-hub/agent"
	"github.com/WWaynee/content-hub/agent/verifier"
	"github.com/WWaynee/content-hub/llmclient"
)

// FactVerifier 稿件事实断言校验器。
// 能力边界：允许语义等价/同义改写；禁止规模/统计推断（不得从明细自行求和估算）。
//
// P07：不再是"单 LLM 自报 supported/evidence_idx 即采信"。先走确定性规则(verifier.Rule),
// 规则能判数值/原文包含/统计禁用才下结论；仅当规则判不了(疑纯语义同义)才以 low_confidence 降级
// 给 LLM 做近义核对。任何判 supported 都必须给出可引证据下标(EvidenceIdx>=0),
// 拿不出可引证据的断言一律不当作 supported 写入 bound。
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
	// Reason 判定依据（规则命中/降级 LLM 近义/待复核），供 P04/P11 展示。
	Reason string `json:"reason,omitempty"`
	// LowConfidence 是否仅凭 LLM 近义判定（确定性规则判不了才可能为 true）。
	LowConfidence bool `json:"low_confidence,omitempty"`
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
//
// P07：supported/evidence_idx 由规则优先决定；
// 仅规则 LowConf(疑似纯语义同义)才降级一次 LLM 近义，且 near-match 命中才 supported(带 idx)。
// 无法拿出可引证据的断言一律记为不支撑(Blocked)，绝不静默放行。
func (v *FactVerifier) Check(ctx context.Context, flatSentences []string, evidence []agent.Evidence) (*FactCheckResult, error) {
	if len(flatSentences) == 0 {
		return &FactCheckResult{}, nil
	}
	prompt := buildVerifyPrompt(flatSentences, evidence)
	// 这一步走 LLM 的目的只是"拆出每个句子里的数据/事实断言文本"；下方 supported 不采信其自报布尔。
	var out struct {
		Sentences []struct {
			Index        int              `json:"index"`
			Text         string           `json:"text"`
			HasDataAssertion bool         `json:"has_data_assertion"`
			Assertions   []struct {
				Text string `json:"text"`
			} `json:"assertions"`
		} `json:"sentences"`
	}
	if err := v.llm.ChatWithJSON(ctx, []llmclient.ChatMessage{{Role: "user", Content: prompt}}, &out); err != nil {
		return nil, fmt.Errorf("事实断言提取失败: %w", err)
	}

	// 候选证据原文（顺序与 evidence 对齐）
	srcTexts := make([]string, len(evidence))
	for i, e := range evidence {
		srcTexts[i] = e.SourceText
	}
	var rule verifier.Rule

	res := &FactCheckResult{}
	for _, raw := range out.Sentences {
		sc := SentenceFactCheck{
			Index:            raw.Index,
			Text:             raw.Text,
			HasDataAssertion: raw.HasDataAssertion,
		}
		for _, a := range raw.Assertions {
			if a.Text == "" {
				continue
			}
			ac, err := decAssertion(ctx, v, rule, a.Text, srcTexts)
			if err != nil {
				return nil, err
			}
			sc.Assertions = append(sc.Assertions, ac)
		}
		res.Sentences = append(res.Sentences, sc)
	}

	// 汇总：凡有断言不支撑(或拿不到可引证据) → Blocked + 收起可读文本
	for _, s := range res.Sentences {
		if !s.HasDataAssertion {
			continue
		}
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

// decAssertion 对单条断言做规则优先的 supported/非 supported 判定；LowConf 再降级 LLM 近义兜底。
func decAssertion(ctx context.Context, v *FactVerifier, rule verifier.Rule, text string, srcTexts []string) (AssertionCheck, error) {
	r := rule.Decide(text, srcTexts)
	switch r.Verdict {
	case verifier.Cover:
		return AssertionCheck{Text: text, Supported: true, EvidenceIdx: r.EvidenceIdx, Reason: r.Reason}, nil
	case verifier.UnsupportedRule:
		return AssertionCheck{Text: text, Supported: false, EvidenceIdx: -1, Reason: r.Reason}, nil
	case verifier.LowConf:
		// 规则判不了 → 用一次 LLM 纯"是否与某条证据语义等值"的近义判定（low_confidence）。
		idx, ok, err := v.nearEqual(ctx, text, srcTexts)
		if err != nil {
			return AssertionCheck{Text: text, Supported: false, EvidenceIdx: -1,
				Reason: "规则未命中且近义核对出错，按不可靠处理(待复核)"}, nil
		}
		if ok && idx >= 0 {
			return AssertionCheck{Text: text, Supported: true, EvidenceIdx: idx,
				Reason: "LLM 低置信近义命中证据原文", LowConfidence: true}, nil
		}
		return AssertionCheck{Text: text, Supported: false, EvidenceIdx: -1,
			Reason: "无规则命中且未能给出可靠近义支持(待复核)"}, nil
	}
	return AssertionCheck{Text: text, Supported: false, EvidenceIdx: -1, Reason: "unknown verdict"}, nil
}

// nearEqual 以 low_confidence 为题，请 LLM 判定 testText 是否与某条证据 semantically over 相等，(仅)返回匹配的证据下标。
func (v *FactVerifier) nearEqual(ctx context.Context, testText string, srcTexts []string) (int, bool, error) {
	if len(srcTexts) == 0 {
		return -1, false, nil
	}
	var b strings.Builder
	b.WriteString("判断下面待核实的表述能否被其中某份原文语义支撑（同义改写可接受，只做低置信近义核对，不做因果/统计）。\n\n")
	b.WriteString("待核实表述：" + testText + "\n\n备选原文：\n")
	for i, s := range srcTexts {
		fmt.Fprintf(&b, "[%d] %s\n", i, s)
	}
	b.WriteString("\n只返回 JSON：{\"supported\":true,\"evidence_idx\":0} 或 {\"supported\":false}")
	var out struct {
		Supported    bool `json:"supported"`
		EvidenceIdx  int  `json:"evidence_idx"`
	}
	if err := v.llm.ChatWithJSON(ctx, []llmclient.ChatMessage{{Role: "user", Content: b.String()}}, &out); err != nil {
		return -1, false, nil // 失败不默认放行，另作待复核
	}
	if !out.Supported || out.EvidenceIdx < 0 || out.EvidenceIdx >= len(srcTexts) {
		return -1, false, nil
	}
	return out.EvidenceIdx, true, nil
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
