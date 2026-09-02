// Package censor 实现稿件生成/修订的「证据合规校验」能力。
//
// 系统能力边界（产品约束）：
//   - 只认「能直接检索到原文」的证据，禁止模型对知识库明细做统计求和、估算、脑补数字。
//   - 稿件中每一处「事实/数据断言」（数字、占比、日期、范围、条款等）必须能直接回溯到
//     某条已检索证据的原文（允许语义等价 / 同义改写，禁止规模/统计推断）。
//   - 任一必需的事实支撑点无证据 → 禁止生成或修订；不含数据断言的一般性公文句放行。
//
// Censor 由 Orchestrator 在「检索后 / 撰写后」两个时点调用：
//  1. Check 撰写前：拆子需求点(claim) + 逐点检索 → 覆盖度核对（缺证清单→阻断）。
//  2. VerifyFacts 撰写后：提取稿件全部数据断言，核对是否各有直接证据支撑。
package censor

import (
	"context"
	"fmt"

	"github.com/WWaynee/content-hub/agent"
	"github.com/WWaynee/content-hub/llmclient"
)

// Claim 一个子需求点（需求单中被要求必须给出的事实/数据支撑单元）。
type Claim struct {
	// Text 子需求的自然语言描述（用于检索 hint 与缺证提示）。
	Text string `json:"text"`
	// NeedsFact 是否需要事实/数据支撑。
	// - true：该点必须能从知识库找到证据，否则阻断（A有B无时，B无即拦截）。
	// - false：纯公文性表述，无需证据，不参与缺证阻断。
	NeedsFact bool `json:"needs_fact"`
	// QueryHint 检索该点线索（LLM 提炼的关键词/问题），供逐点检索。
	QueryHint string `json:"query_hint"`
}

// Searcher 单条 query 的句子级检索能力（由上层注入，避免 censor 依赖 service/storage）。
type Searcher interface {
	// SearchSentences 检索并返回「句子级」证据；fileIDs 空 = 全租户。
	SearchSentences(ctx context.Context, tenantID uint64, query string, fileIDs ...uint64) ([]agent.Evidence, error)
}

// ClaimCoverage 一次「子需求点覆盖度核对」的结果。
type ClaimCoverage struct {
	// Missing 未能从知识库获得证据的「需要事实支撑」的子需求点。
	Missing []Claim
	// Evidence 所有已核对成功的证据（逐点检索去重合并）。
	Evidence []agent.Evidence
	// Queries 实际执行过的检索 query（供审计/排查）。
	Queries []string
}

// ClaimPlanner 子需求点拆解器。
type ClaimPlanner struct {
	llm      llmclient.Client
	searcher Searcher
}

// NewClaimPlanner 构造。
func NewClaimPlanner(llm llmclient.Client, searcher Searcher) *ClaimPlanner {
	return &ClaimPlanner{llm: llm, searcher: searcher}
}

// Check 拆分需求→逐点检索→覆盖度核对，返回缺证清单与已核对证据。
func (p *ClaimPlanner) Check(ctx context.Context, tenantID uint64, r agent.Requirement, fileIDs []uint64) (*ClaimCoverage, error) {
	claims, err := p.PlanClaims(ctx, r)
	if err != nil {
		return nil, err
	}

	cov := &ClaimCoverage{}
	seen := map[uint64]bool{} // 按 doc_sentence_id 去重
	for _, c := range claims {
		// 只需事实支撑的点才参与检索与缺证核对
		if !c.NeedsFact {
			continue
		}
		q := c.QueryHint
		if q == "" {
			q = c.Text
		}
		cov.Queries = append(cov.Queries, q)
		hits, err := p.searcher.SearchSentences(ctx, tenantID, q, fileIDs...)
		if err != nil {
			return nil, fmt.Errorf("检索子需求点 %q 失败: %w", c.Text, err)
		}
		// 该点无证据 → 记为缺证（保留，用于缺证清单）
		if len(hits) == 0 {
			cov.Missing = append(cov.Missing, c)
			continue
		}
		for _, h := range hits {
			if seen[h.DocSentenceID] {
				continue
			}
			seen[h.DocSentenceID] = true
			cov.Evidence = append(cov.Evidence, h)
		}
	}
	return cov, nil
}

// PlanClaims 把需求单拆成子需求点列表。
func (p *ClaimPlanner) PlanClaims(ctx context.Context, r agent.Requirement) ([]Claim, error) {
	prompt := buildClaimPrompt(r)
	var out struct {
		Claims []Claim `json:"claims"`
	}
	if err := p.llm.ChatWithJSON(ctx, []llmclient.ChatMessage{{Role: "user", Content: prompt}}, &out); err != nil {
		return nil, fmt.Errorf("拆解子需求点失败: %w", err)
	}
	var claims []Claim
	for _, c := range out.Claims {
		if c.Text != "" {
			claims = append(claims, c)
		}
	}
	if len(claims) == 0 {
		return nil, fmt.Errorf("LLM 未拆解出有效子需求点")
	}
	return claims, nil
}

func buildClaimPrompt(r agent.Requirement) string {
	return fmt.Sprintf(`你是一个政务稿件合规分析助手。请把下面的稿件需求，拆成若干「子需求点」——即稿件的每个必须交代清楚的具体内容点（事实、数据、规定、对象、范围等）。

稿件需求：
- 标题：%s
- 发文主体：%s
- 发文目的：%s
- 目标受众：%s
- 章节要求：%s
- 字数要求：%d

要求：
1. 每个子需求点都要明确标注 needs_fact（是否必须依赖知识库的事实/数据/规定原文才能写）。
   - 例如「公司全年演出场次」「某艺人票房」「某项规定的天数/比例」→ needs_fact=true。
   - 例如「强调重要性」「号召全体员工配合」「总体回顾」这类不含具体事实数据、用通用语言即可的 → needs_fact=false。
2. 为每个 needs_fact=true 的点提供 query_hint（最可能检索到该点资料的检索词）。
3. 只返回 JSON：{"claims":[{"text":"子需求点描述","needs_fact":true,"query_hint":"检索词"}]}，8 条以内。`,
		r.Title, r.StyleSubject, r.StylePurpose, r.StyleAudience, r.ChapterRequirement, r.WordCount)
}

