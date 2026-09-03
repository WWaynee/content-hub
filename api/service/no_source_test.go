package service

import (
	"testing"

	"github.com/WWaynee/content-hub/storage/model"
)

// P09 纯单测：claim_type 对"无源占位(vs 真 source)"的读出优先级 + sequence 编辑产生 unsourced 标记。

// fakeSrcBinds 三句：句1 bound(真 source)，句2 无绑定(plain)，句3 带 no_source 占位。
func fakeViews(t *testing.T, withMark string) []SentenceView {
	t.Helper()
	const verID = uint64(9)
	sents := []model.ArticleSentence{
		{ID: 101, ArticleVersionID: verID, SectionIndex: 0, ParagraphIndex: 0, SentenceIndex: 0, Content: "有据句"},
		{ID: 102, ArticleVersionID: verID, SectionIndex: 0, ParagraphIndex: 0, SentenceIndex: 1, Content: "通稿句"},
		{ID: 103, ArticleVersionID: verID, SectionIndex: 0, ParagraphIndex: 0, SentenceIndex: 2, Content: "无源句子"},
	}
	binds := []model.EvidenceBinding{
		{ArticleSentenceID: 101, SourceType: "knowledge", DocFileID: 1, DocSentenceID: 9001, EvidenceStatus: "bound", OrderNo: 0},
		{ArticleSentenceID: 103, SourceType: "knowledge", EvidenceStatus: withMark, OrderNo: 0}, // doc=0 占位
	}
	sourceBySent := map[uint64][]SourceView{
		101: {{DocSentenceID: 9001, SourceText: "依据", FileName: "x.md"}},
		// 102 无源 → plausible；103 占位 → 即便 sources 空也不该是 plausible
	}
	return BuildSentenceViews(sents, sourceBySent, ClaimStatusBySent(binds))
}

func TestNoSourceViews_ClaimPriority(t *testing.T) {
	views := fakeViews(t, "no_source")
	if len(views) != 3 {
		t.Fatalf("应 3 视图")
	}
	if views[0].ClaimType != ClaimTypeBound {
		t.Errorf("句1 应 bound，got %s", views[0].ClaimType)
	}
	if views[1].ClaimType != ClaimTypePlausibleAI {
		t.Errorf("句2(无绑定) 应 plausible-ai，got %s", views[1].ClaimType)
	}
	if views[2].ClaimType != ClaimTypeNoSource {
		t.Errorf("句3(no_source 占位) 应标 no_source，got %q", views[2].ClaimType)
	}
}

func TestNoSourceViews_HumanKept(t *testing.T) {
	views := fakeViews(t, "human_kept")
	if views[2].ClaimType != ClaimTypeHumanKept {
		t.Errorf("human_kept 占位对应 human_kept，got %q", views[2].ClaimType)
	}
}

func TestSequence_InsertMarksUnsourcedHelper(t *testing.T) {
	src, bind := mkSrcSet() // 全是"(0,0)"普通句
	p, err := applyChangePlan(src, bind, &ChangeListRequest{Ops: []ChangeOp{
		{Op: "insert", AnchorID: 2, NewText: "这句人新加的没来源"},
	}}, 9)
	if err != nil {
		t.Fatal(err)
	}
	// 新句应为 unsourced=true（供落库写 no_source 占位）
	for _, s := range p.sents {
		if s.content == "这句人新加的没来源" {
			if !s.unsourced {
				t.Errorf("insert 无证句应被标记 unsourced（P09 落 no_source）")
			}
			return
		}
	}
	t.Fatal("未找到插入句")
}
