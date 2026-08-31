package orchestrator

import (
	"context"
	"fmt"

	"github.com/WWaynee/content-hub/agent"
)

// SentenceRewriter 句子级重写接口（撰写 agent 实现，只重写指定句子）。
type SentenceRewriter interface {
	// RewriteSentence 重写目标句子，返回新句子文本 + 该句引用哪些证据（evidence 索引）。
	RewriteSentence(ctx context.Context, req agent.WritingRequest, targetIndex int, instruction string) (newText string, evidenceRefs []uint64, err error)
}

// Reviser 句子级修订器（局部重写 + 未动句继承 + 被改句重检测，方案甲）。
type Reviser struct {
	rewriter  SentenceRewriter
	retriever Retriever
}

// NewReviser 构造修订器。
func NewReviser(rw SentenceRewriter, r Retriever) *Reviser {
	return &Reviser{rewriter: rw, retriever: r}
}

// RevisionResult 一次局部修订的结果。
type RevisionResult struct {
	SentenceIndex int               // 被改的句子序号
	NewText       string            // 改后文本
	EvidenceRefs  []uint64          // 被改句重新检索后绑定的证据（evidence 索引）
	NewEvidence   []agent.Evidence  // 该句重新检索到的新证据（供落库）
	OldEvidence   []uint64          // 改前该句绑定（调用方记录，用于 diff/审计）
}

// ReviseSentence 修订指定句子：只重写该句 → 该句重新检索证据（方案甲）。
// 未改句子由调用方保持原样（继承原证据）。
// scopeFileIDs：勾选范围（传给检索，锁定检索范围）。
func (rv *Reviser) ReviseSentence(ctx context.Context, tenantID uint64, req agent.Requirement, targetIndex int, instruction string, allEvidence []agent.Evidence, scopeFileIDs []uint64) (*RevisionResult, error) {
	// 1. 句子级重写
	newText, refs, err := rv.rewriter.RewriteSentence(ctx, agent.WritingRequest{
		Requirement: req,
		Evidence:    allEvidence,
	}, targetIndex, instruction)
	if err != nil {
		return nil, fmt.Errorf("句子重写失败: %w", err)
	}
	if newText == "" {
		return nil, fmt.Errorf("撰写 agent 未返回重写文本")
	}

	// 2. 被改句重新检索证据（方案甲）
	ret, err := rv.retriever.Retrieve(ctx, agent.RetrieveRequest{
		TenantID:    tenantID,
		Requirement: req,
		FileIDs:     scopeFileIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("被改句重检测证据失败: %w", err)
	}

	return &RevisionResult{
		SentenceIndex: targetIndex,
		NewText:       newText,
		EvidenceRefs:  refs,
		NewEvidence:   ret.Evidence,
	}, nil
}
