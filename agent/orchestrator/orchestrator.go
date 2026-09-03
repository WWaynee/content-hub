// Package orchestrator 实现 content-hub 的多 Agent 工作流编排。
//
// 采用「全局编排 + 局部自主」：Orchestrator 按确定的工作流调度各 agent，
// agent 之间通过结构化数据（agent 包的类型）传递，不互相自由指挥。
package orchestrator

import (
	"context"
	"errors"
	"fmt"

	"github.com/WWaynee/content-hub/agent"
	"github.com/WWaynee/content-hub/agent/censor"
)

// ErrNoEvidence 检索阶段未能从知识库中获取到任何与该需求主题相关的资料。
// 此时不应让撰写 agent 凭空生成（否则会编造数据），而是向上返回此错误，
// 由调用方转为明确的业务提示。
var ErrNoEvidence = errors.New("知识库中未检索到与该需求主题相关的资料")

// ErrInsufficientEvidence 存在「需要事实/数据支撑」但知识库无证据可支撑的子需求点。
// 携带缺证清单（Missing），供调用方明确告知用户缺哪些点。
type ErrInsufficientEvidence struct {
	Missing []censor.Claim
}

func (e *ErrInsufficientEvidence) Error() string {
	if len(e.Missing) == 0 {
		return "知识库资料不足，无法生成稿件"
	}
	return fmt.Sprintf("以下内容在知识库中未检索到支撑资料：%v", e.Missing)
}

// ErrFactUnsupported 稿件中存在「含数据/事实断言但无法在证据原文中找到直接支撑」的句子。
// 按能力边界应阻断生成（禁止把无源数据写进稿件）。
type ErrFactUnsupported struct {
	Unsupported []string
}

func (e *ErrFactUnsupported) Error() string {
	if len(e.Unsupported) == 0 {
		return "稿件包含无法在知识库中找到支撑的数据断言"
	}
	return fmt.Sprintf("稿件包含无法在知识库中找到支撑的数据断言：%v", e.Unsupported)
}

// Retriever 知识检索 agent 接口。
type Retriever interface {
	Retrieve(ctx context.Context, req agent.RetrieveRequest) (*agent.RetrieveResult, error)
}

// Writer 稿件撰写 agent 接口。
type Writer interface {
	Write(ctx context.Context, req agent.WritingRequest) (*agent.Article, error)
}

// EvidenceBuilder 证据整理 agent 接口。
type EvidenceBuilder interface {
	Build(ctx context.Context, article *agent.Article, evidence []agent.Evidence) (*agent.EvidenceManifest, error)
}

// Orchestrator 工作流编排器。
type Orchestrator struct {
	retriever Retriever
	writer    Writer
	evidence  EvidenceBuilder
	checker   *censor.ClaimPlanner
	verifier  *censor.FactVerifier
}

// New 构造编排器。
// checker 为子需求点覆盖度核对器（可为 nil，此时退化为旧的整体无证据拦截）。
func New(r Retriever, w Writer, e EvidenceBuilder, checker *censor.ClaimPlanner) *Orchestrator {
	return &Orchestrator{retriever: r, writer: w, evidence: e, checker: checker}
}

// SetFactVerifier 设置撰写后的事实断言校验器（可为 nil = 跳过闸门二）。
func (o *Orchestrator) SetFactVerifier(v *censor.FactVerifier) *Orchestrator {
	o.verifier = v
	return o
}

// GenerationResult 一次稿件生成（generation）的完整产物。
type GenerationResult struct {
	Article  *agent.Article
	Evidence []agent.Evidence
	Manifest *agent.EvidenceManifest
	Queries  []string
}

// Generate 执行「需求 → 检索 → 撰写 → 证据」完整 generation 工作流。
func (o *Orchestrator) Generate(ctx context.Context, tenantID uint64, req agent.Requirement, fileIDs []uint64) (*GenerationResult, error) {
	// 全局检索只作为"未注入 claim 覆盖核对器(checker)"时的兜底资源；
	// 一旦注入了 checker，就以 claim 逐点检索为准，避免 Generate 开头那一次对全需求重复首检(C6 冗余)。
	var finalEvidence []agent.Evidence
	var finalQueries []string
	if o.checker != nil {
		cov, cerr := o.checker.Check(ctx, tenantID, req, fileIDs)
		if cerr != nil {
			return nil, cerr
		}
		// 存在「需要事实支撑」但无证据的点 → 整篇阻断，返回缺证清单(由上游转 await_human/缺证人话)
		if len(cov.Missing) > 0 {
			return nil, &ErrInsufficientEvidence{Missing: cov.Missing}
		}
		if len(cov.Evidence) == 0 {
			return nil, ErrNoEvidence
		}
		finalEvidence = cov.Evidence
		finalQueries = cov.Queries
	} else {
		// 旧兼容：无 checker 时退化为"整体检索 + 有无证据"的简单闸(不逐点核对)。
		if o.retriever == nil {
			return nil, ErrNoEvidence
		}
		ret, rerr := o.retriever.Retrieve(ctx, agent.RetrieveRequest{
			TenantID: tenantID, Requirement: req, FileIDs: fileIDs,
		})
		if rerr != nil {
			return nil, rerr
		}
		finalEvidence = ret.Evidence
		finalQueries = ret.Queries
		if len(finalEvidence) == 0 {
			return nil, ErrNoEvidence
		}
	}

	// 2. 撰写
	article, err := o.writer.Write(ctx, agent.WritingRequest{
		Requirement: req,
		Evidence:    finalEvidence,
	})
	if err != nil {
		return nil, err
	}

	// 2.5 事实断言校验（闸门二）：稿件的每个数据/事实断言必须能直接在证据原文中找到支撑。
	//      允许语义等价/同义改写，禁止规模统计推断。有无法支撑的数据断言 → 阻断，不落库。
	if o.verifier != nil {
		flat := flattenSentences(article)
		fc, ferr := o.verifier.Check(ctx, flat, finalEvidence)
		if ferr != nil {
			return nil, ferr
		}
		if fc.Blocked {
			return nil, &ErrFactUnsupported{Unsupported: fc.UnsupportedTexts}
		}
		// 校验通过后，把每个句子的实际证据绑定（evidence_idx）回写到 article，供证据整理使用
		if err := applyFactRefs(article, finalEvidence, fc); err != nil {
			return nil, err
		}
	}

	// 3. 证据整理
	manifest, err := o.evidence.Build(ctx, article, finalEvidence)
	if err != nil {
		return nil, err
	}

	return &GenerationResult{
		Article:  article,
		Evidence: finalEvidence,
		Manifest: manifest,
		Queries:  finalQueries,
	}, nil
}

// flattenSentences 把稿件按句子顺序平铺成文本列表（与 article 的句子序列一一对应）。
func flattenSentences(a *agent.Article) []string {
	var out []string
	for _, sec := range a.Sections {
		for _, para := range sec.Paragraphs {
			for _, s := range para.Sentences {
				out = append(out, s.Text)
			}
		}
	}
	return out
}

// applyFactRefs 用事实校验结果校准每个句子的证据绑定（evidence_refs）。
// 对校验通过的句子，若其断言引用了证据，则把对应 evidence 索引写入 EvidenceRefs；
// 不含数据断言（纯公文）的句子保持原绑定（可能为空）。
func applyFactRefs(a *agent.Article, _ []agent.Evidence, fc *censor.FactCheckResult) error {
	sentIdx := map[int]*censor.SentenceFactCheck{}
	for i := range fc.Sentences {
		sentIdx[fc.Sentences[i].Index] = &fc.Sentences[i]
	}
	pos := 0
	for si := range a.Sections {
		for pi := range a.Sections[si].Paragraphs {
			for tsi := range a.Sections[si].Paragraphs[pi].Sentences {
				// 若该句含已校验的数据断言，用断言引用的证据重置绑定（取所有 supported 断言的 evidence_idx）
				if chk, ok := sentIdx[pos]; ok && chk.HasDataAssertion {
					var refs []uint64
					seen := map[uint64]bool{}
					for _, as := range chk.Assertions {
						if as.Supported && as.EvidenceIdx >= 0 && !seen[uint64(as.EvidenceIdx)] {
							seen[uint64(as.EvidenceIdx)] = true
							refs = append(refs, uint64(as.EvidenceIdx))
						}
					}
					a.Sections[si].Paragraphs[pi].Sentences[tsi].EvidenceRefs = refs
				}
				pos++
			}
		}
	}
	return nil
}
