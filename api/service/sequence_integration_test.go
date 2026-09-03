//go:build integration

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/WWaynee/content-hub/agent"
	"github.com/WWaynee/content-hub/config"
	"github.com/WWaynee/content-hub/storage"
)

// TestSequenceEdit_EndToEndWithCAS —— P08 change_list 在真 MySQL 上的端到端：
//   - 先 PersistArticleSnapshot 造出一份 v1（A/B 两句，A 有证据取自知识库句）；
//   - 提交一次受控序列编辑(删除 A + 在 B 后插入一句无来源 + 改 B 文本) → 断言 v2 快照、
//     顺序、保留 B 原绑定、被删 A 消失、insert 新句无绑定被保留(reviews 带 no_source)。
//   - 再以过期 base_article_version 提交 → 由 CAS 拒绝，返回 ErrSequenceConflict 且不产生新版本。
func TestSequenceEdit_EndToEndWithCAS(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("配置加载失败: %v", err)
	}
	if _, err := storage.InitMySQL(&cfg.MySQL); err != nil {
		t.Skipf("MySQL 不可用: %v", err)
	}
	ctx := context.Background()
	tenantID := uint64(99990050)
	w, _ := CreateWorkspace(ctx, tenantID, 1, "seq-e2e", nil)

	evidence := []agent.Evidence{{FileID: 11, DocSentenceID: 5001, VersionMd5: "v1", SourceText: "依据内容甲"}}
	article := &agent.Article{
		Title: "稿",
		Sections: []agent.Section{{Paragraphs: []agent.Paragraph{{
			Sentences: []agent.Sentence{
				{Text: "句A被删除", EvidenceRefs: []uint64{0}},
				{Text: "句B会被改又保留", EvidenceRefs: []uint64{}},
			},
		}}}},
	}
	if _, err := PersistArticleSnapshot(ctx, tenantID, w.ID, article, evidence); err != nil {
		t.Fatalf("造 v1 失败: %v", err)
	}

	a, _ := storage.GetArticleByWorkspace(ctx, tenantID, w.ID)
	prev, _ := storage.GetLatestArticleVersion(ctx, a.ID)
	if prev.VersionNo != 1 {
		t.Fatalf("期望基线 v1, got %d", prev.VersionNo)
	}
	sents, _ := storage.ListArticleSentences(ctx, prev.ID)
	if len(sents) != 2 {
		t.Fatalf("v1 应有 2 句, got %d", len(sents))
	}
	idA, idB := sents[0].ID, sents[1].ID

	// 提交 change_list: delete A, insert(无来源, in B 后), edit B 文本
	verID, reviews, serr := RunSequenceEdit(ctx, tenantID, 1, w.ID, &ChangeListRequest{
		Ops: []ChangeOp{
			{Op: "delete", TargetID: idA},
			{Op: "edit", TargetID: idB, NewText: "句B被改又保留-改"},
			{Op: "insert", AnchorID: idB, NewText: "这句没有基础文档支撑"},
		},
	})
	if serr != nil {
		t.Fatalf("序列编辑失败: %v", serr)
	}
	if verID == 0 {
		t.Fatal("未产新版本")
	}

	// 校验新版本 v2
	cur, _ := storage.GetLatestArticleVersion(ctx, a.ID)
	if cur.VersionNo != 2 || cur.ReferencedVersion != 1 {
		t.Errorf("期望 v2(ref v1), got v%d", cur.VersionNo)
	}
	after, _ := storage.ListArticleSentences(ctx, cur.ID)
	if len(after) != 2 {
		t.Fatalf("删1插1改1后应 2 句, got %d", len(after))
	}
	// 顺序: B(改) < 新句(插入其后)
	if after[0].Content != "句B被改又保留-改" || after[1].Content != "这句没有基础文档支撑" {
		t.Errorf("seq 缺序不达: %q | %q", after[0].Content, after[1].Content)
	}
	// A 该消失
	for _, s := range after {
		if s.Content == "句A被删除" {
			t.Error("被删句仍出现在新版本")
		}
	}
	// B 的原绑定应保留（改文本默认不卸来源）
	bindsB, _ := storage.ListArticleBindings(ctx, cur.ID)
	if len(bindsB) != 1 || bindsB[0].DocSentenceID != 5001 {
		t.Errorf("B 绑定应在编辑后被保留, got %d", len(bindsB))
	}
	// insert 无来源应给出 no_source 提醒且正文已保留
	if len(reviews) == 0 {
		t.Error("insert 无来源应给 no_source 提醒")
	}

	// 并发/CAS：用过期 base(=1)再提交一次 —— 现在 current 已是 2,故冲突、不产新版本
	verBefore := currentArticleVersion(ctx, tenantID, w.ID)
	_, _, cerr := RunSequenceEdit(ctx, tenantID, 1, w.ID, &ChangeListRequest{
		BaseArticleVersion: 1,
		Ops:                []ChangeOp{{Op: "edit", TargetID: sents[0].ID, NewText: "越权改"}},
	})
	if cerr == nil {
		t.Fatal("过期 base 应被拒绝")
	}
	if !errors.Is(cerr, ErrSequenceConflict) && !errors.Is(cerr, ErrArticleVersionConflict) {
		t.Errorf("期望 CAS 冲突错误, got %v", cerr)
	}
	if got := currentArticleVersion(ctx, tenantID, w.ID); got != verBefore {
		t.Errorf("被拒请求不应推进版本, before=%d got=%d", verBefore, got)
	}
}
