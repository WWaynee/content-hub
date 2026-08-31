package service

import (
	"context"
	"testing"

	"github.com/WWaynee/content-hub/agent"
	"github.com/WWaynee/content-hub/config"
	"github.com/WWaynee/content-hub/storage"
)

// TestApplyArticleRevision 验证句子级修订：未动句继承、被改句更新、落新快照。
func TestApplyArticleRevision(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("配置加载失败: %v", err)
	}
	if _, err := storage.InitMySQL(&cfg.MySQL); err != nil {
		t.Skipf("MySQL 不可用: %v", err)
	}
	ctx := context.Background()
	tenantID := uint64(99992010)

	w, _ := CreateWorkspace(ctx, tenantID, 1, "修订测试", nil)

	// 构造证据 + 2 句稿件
	evidence := []agent.Evidence{
		{FileID: 100, DocSentenceID: 1001, VersionMd5: "v1", SourceText: "原文A"},
		{FileID: 200, DocSentenceID: 2002, VersionMd5: "v2", SourceText: "原文B"},
	}
	article := &agent.Article{Title: "稿", Sections: []agent.Section{{Paragraphs: []agent.Paragraph{{
		Sentences: []agent.Sentence{
			{Text: "句0", EvidenceRefs: []uint64{0}},
			{Text: "句1", EvidenceRefs: []uint64{1}},
		},
	}}}}}
	verID, err := PersistArticleSnapshot(ctx, tenantID, w.ID, 1, article, evidence)
	if err != nil {
		t.Fatalf("落初稿快照失败: %v", err)
	}

	// 修订句0：新文本 + 新证据（只有一个新证据doc=300）
	newEvidence := []agent.Evidence{{FileID: 300, DocSentenceID: 3003, VersionMd5: "v3", SourceText: "原文C"}}
	newVerID, err := ApplyArticleRevision(ctx, ReviseSentenceInput{
		WorkspaceID:     w.ID,
		TenantID:        tenantID,
		TargetIndex:     0,
		NewText:         "句0改",
		NewEvidence:     newEvidence,
		NewEvidenceRefs: []uint64{0},
	})
	if err != nil {
		t.Fatalf("ApplyArticleRevision 失败: %v", err)
	}
	if newVerID == verID {
		t.Errorf("新快照 ID 应不同于旧快照")
	}

	// 验证新快照：句0=句0改(绑新证据3003)，句1=句1(继承原证据2002)
	sents, _ := storage.ListArticleSentences(ctx, newVerID)
	if len(sents) != 2 {
		t.Fatalf("新快照应 2 句，实际 %d", len(sents))
	}
	if sents[0].Content != "句0改" || sents[1].Content != "句1" {
		t.Errorf("句子内容不符: [0]=%q [1]=%q", sents[0].Content, sents[1].Content)
	}
	binds, _ := storage.ListArticleBindings(ctx, newVerID)
	// 找到句1 的绑定（继承 2002）
	foundInherited := false
	foundNew := false
	for _, b := range binds {
		if b.DocSentenceID == 2002 {
			foundInherited = true
		}
		if b.DocSentenceID == 3003 {
			foundNew = true
		}
	}
	if !foundInherited {
		t.Errorf("未动句1 应继承证据 2002")
	}
	if !foundNew {
		t.Errorf("被改句0 应绑定新证据 3003")
	}

	// 清理
	db := storage.GetDB()
	db.Exec("DELETE FROM evidence_bindings WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM article_sentences WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM article_versions WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM articles WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM requirements WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM workspaces WHERE tenant_id = ?", tenantID)
}
