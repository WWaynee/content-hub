package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/WWaynee/content-hub/agent/censor"
	"github.com/WWaynee/content-hub/llmclient"
	"github.com/WWaynee/content-hub/storage"
)

// manual_govern.go — P09(治理补充)：对一句“可能需要对外表态”的手编文本，跑一次真校验并归三态：
//
//	bound     —— 有断言且 P07 规则/近义能给到可引 Evidence → 带真源(doc)做 bound。
//	no_source —— 有断言但知识库(规则+近义检索)给不出可引 → 黄点，交作者取舍（不硬删、不伪称有据）。
//	plausible —— 纯措辞/衔接、并不构成需外部依据的数据断言 → 不带证据也不标黄(AvoidNoSource)。
//
// 与 P07 revise 共用同一份 censor.FactVerifier.Check（LLM 只负责"拆出该句的断言"，是否被真证据支撑
// 由 Rule 优先定，仅在 LowConf 才降一次 LLM 近义）——因此这不是让模型自报。
//
// 连不上 LLM/检索时（离线/CI/服务未配）走保守 fallback：按 no_source 保留并给一句人话，不在链路上夹断用户编辑；
// 至少决不会把一个没来源的新内容偷偷标成"有据"。

// GovernSource 治理认出一句可附的证据引用（喂给 ChangeOp.Evidence）。
type GovernSource struct {
	FileID        uint64 `json:"file_id"`
	DocSentenceID uint64 `json:"doc_sentence_id"`
	SourceType    string `json:"source_type,omitempty"`
}

// GovernResult 治理单条文本后的整体结论（供上层组 op）。
type GovernResult struct {
	Text      string         `json:"text"`
	ClaimType string         `json:"claim_type"` // bound | no_source | plausible
	Sources   []GovernSource `json:"sources,omitempty"`
	HumanText string         `json:"human_text"`
	Fallback  bool           `json:"fallback,omitempty"`
}

// GovernManualSentence 对一条手编文本做治理，返回 claim 结论。
func GovernManualSentence(ctx context.Context, tenantID uint64, wsID uint64, text string) (*GovernResult, error) {
	out := &GovernResult{Text: text}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("治理需要一句非空文本")
	}

	// 1) 检索范围(需求单勾选;空=不限定) + 候选证据
	var fileIDs []uint64
	if req, err := storage.GetRequirementByWorkspace(ctx, tenantID, wsID); err == nil {
		if ids, serr := RequirementFileIDScope(ctx, tenantID, req.ID); serr == nil {
			fileIDs = ids
		}
	}
	hits, herr := SearchKbaseSentences(ctx, tenantID, text, fileIDs...)
	if herr != nil {
		return fallbackGovern(out, "本次检索该工作区的参考资料失败（"+herr.Error()+"）；未能确认来源，已按“无外部依据·待你复核”保留，不替你删字。")
	}
	evidence := hitsToEvidence(hits)

	// 2) 剥断言 + P07 规则/近义判 supported
	checker := censor.NewFactVerifier(llmclient.NewClient())
	fc, verr := checker.Check(ctx, []string{text}, evidence)
	if verr != nil {
		return fallbackGovern(out, "事实校验链路暂不可达（"+verr.Error()+"）；按‘无外部依据·待你复核’处理，不会硬删你编辑的内容。")
	}

	// 3) 按句归类
	if len(fc.Sentences) == 0 || !fc.Sentences[0].HasDataAssertion {
		// 纯措辞/衔接：没有需要核对的数/事实 → 无需给据、也不黄
		out.ClaimType = ClaimTypePlausibleAI
		out.HumanText = "这句话是措辞/衔接语，不构成需要外部依据的数据断言，作为普通通稿句保留。"
		return out, nil
	}

	refs := []GovernSource{}
	for _, a := range fc.Sentences[0].Assertions {
		if !a.Supported || a.EvidenceIdx < 0 || a.EvidenceIdx >= len(evidence) {
			continue
		}
		e := evidence[a.EvidenceIdx]
		refs = append(refs, GovernSource{FileID: e.FileID, DocSentenceID: e.DocSentenceID, SourceType: "knowledge"})
	}
	refs = dedupeGovernSources(refs)
	if len(refs) > 0 {
		out.ClaimType = ClaimTypeBound
		out.Sources = refs
		out.HumanText = fmt.Sprintf("这句话含数据/事实断言，已为它找到 %d 处来自知识库的可引来源，可作有据句。", len(refs))
		return out, nil
	}

	out.ClaimType = ClaimTypeNoSource
	out.HumanText = "这句话含有需要依据的数据/事实，但当前知识库(规则+检索)拿不到可引来源。正文已按“无外部依据·待你复核”保留，你可以三选：认可保留 / 补资料后更新 / 删除。"
	return out, nil
}

func fallbackGovern(out *GovernResult, m string) (*GovernResult, error) {
	out.ClaimType = ClaimTypeNoSource
	out.Fallback = true
	out.HumanText = m
	return out, nil
}

func dedupeGovernSources(in []GovernSource) []GovernSource {
	seen := map[uint64]bool{}
	var out []GovernSource
	for _, g := range in {
		if g.DocSentenceID == 0 || seen[g.DocSentenceID] {
			continue
		}
		seen[g.DocSentenceID] = true
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DocSentenceID < out[j].DocSentenceID })
	return out
}
