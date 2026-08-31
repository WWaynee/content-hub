package orchestrator

import (
	"context"
	"testing"

	"github.com/WWaynee/content-hub/agent"
)

// stubRewriter 实现 SentenceRewriter，返回固定结果。
type stubRewriter struct {
	newText string
	refs    []uint64
}

func (s *stubRewriter) RewriteSentence(ctx context.Context, req agent.WritingRequest, targetIndex int, instruction string) (string, []uint64, error) {
	return s.newText, s.refs, nil
}

// stubRetriever 实现 Retriever，返回固定证据。
type stubRetriever struct {
	evidence []agent.Evidence
}

func (s *stubRetriever) Retrieve(ctx context.Context, req agent.RetrieveRequest) (*agent.RetrieveResult, error) {
	return &agent.RetrieveResult{Evidence: s.evidence}, nil
}

// 验证「未动句继承」：ApplySentenceRevision 只更新被改句，其余句原样保留。
func TestApplySentenceRevision_InheritsUntouched(t *testing.T) {
	orig := []agent.Sentence{
		{Text: "句0", EvidenceRefs: []uint64{0}},
		{Text: "句1", EvidenceRefs: []uint64{1}},
		{Text: "句2", EvidenceRefs: []uint64{2}},
	}
	got := ApplySentenceRevision(orig, 1, "句1改", []uint64{9})
	if got[0].Text != "句0" || got[0].EvidenceRefs[0] != 0 {
		t.Errorf("未改句0 应原样继承，实际 %+v", got[0])
	}
	if got[1].Text != "句1改" || got[1].EvidenceRefs[0] != 9 {
		t.Errorf("被改句1 应更新，实际 %+v", got[1])
	}
	if got[2].Text != "句2" || got[2].EvidenceRefs[0] != 2 {
		t.Errorf("未改句2 应原样继承，实际 %+v", got[2])
	}
}

// 验证 Reviser 的"重写 + 重检测"链路（stub 隔离）。
func TestReviser_ReviseSentence(t *testing.T) {
	rw := &stubRewriter{newText: "新句子", refs: []uint64{0}}
	rr := &stubRetriever{evidence: []agent.Evidence{{DocSentenceID: 77, SourceText: "原文"}}}
	rv := NewReviser(rw, rr)

	req := agent.Requirement{Title: "t"}
	res, err := rv.ReviseSentence(context.Background(), 1, req, 2, "改一下", nil, nil)
	if err != nil {
		t.Fatalf("ReviseSentence 失败: %v", err)
	}
	if res.NewText != "新句子" {
		t.Errorf("NewText 应为新句子，实际 %q", res.NewText)
	}
	if res.SentenceIndex != 2 {
		t.Errorf("SentenceIndex 应为 2，实际 %d", res.SentenceIndex)
	}
	if len(res.NewEvidence) != 1 || res.NewEvidence[0].DocSentenceID != 77 {
		t.Errorf("应重检测到 1 条新证据(doc_sentence_id=77)，实际 %+v", res.NewEvidence)
	}
}
