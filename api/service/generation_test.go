package service

import (
	"context"
	"testing"

	"github.com/WWaynee/content-hub/agent"
	"github.com/WWaynee/content-hub/config"
	"github.com/WWaynee/content-hub/storage"
	"github.com/WWaynee/content-hub/storage/model"
)

// TestPersistArticleSnapshot 验证稿件快照落库 + 句级证据绑定正确。
// 不跑真实 LLM，直接构造结构化稿件 + 证据，测落库链路。
func TestPersistArticleSnapshot(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("配置加载失败: %v", err)
	}
	if _, err := storage.InitMySQL(&cfg.MySQL); err != nil {
		t.Skipf("MySQL 不可用: %v", err)
	}
	ctx := context.Background()
	tenantID := uint64(99990030)

	w, _ := CreateWorkspace(ctx, tenantID, 1, "快照测试", nil)

	// 构造 2 条句子级证据
	evidence := []agent.Evidence{
		{FileID: 100, DocSentenceID: 1001, VersionMd5: "v1", SourceText: "原文A"},
		{FileID: 200, DocSentenceID: 2002, VersionMd5: "v2", SourceText: "原文B"},
	}
	// 稿件：2 个句子，第 1 句绑证据0，第 2 句绑证据1
	article := &agent.Article{
		Title: "测试稿件",
		Sections: []agent.Section{{
			Heading: "第一章",
			Paragraphs: []agent.Paragraph{{
				Sentences: []agent.Sentence{
					{Text: "句子一。", EvidenceRefs: []uint64{0}},
					{Text: "句子二。", EvidenceRefs: []uint64{1}},
				},
			}},
		}},
	}

	verID, err := PersistArticleSnapshot(ctx, tenantID, w.ID, article, evidence)
	if err != nil {
		t.Fatalf("PersistArticleSnapshot 失败: %v", err)
	}
	if verID == 0 {
		t.Fatal("应返回 article_version ID")
	}

	// 验证 sentences 落库（2 条）
	var sents []model.ArticleSentence
	storage.GetDB().Where("article_version_id = ?", verID).Order("sentence_index").Find(&sents)
	if len(sents) != 2 {
		t.Fatalf("应落 2 条句子，实际 %d", len(sents))
	}
	// 验证 bindings 落库（2 条，指向正确 doc_sentence_id + 正确 article_sentence_id）
	var binds []model.EvidenceBinding
	storage.GetDB().Where("article_version_id = ?", verID).Order("order_no").Find(&binds)
	if len(binds) != 2 {
		t.Fatalf("应落 2 条证据绑定，实际 %d", len(binds))
	}
	if binds[0].DocSentenceID != 1001 || binds[1].DocSentenceID != 2002 {
		t.Fatalf("证据绑定 doc_sentence_id 不符: %+v", binds)
	}
	if binds[0].ArticleSentenceID != sents[0].ID {
		t.Errorf("binding0 应指向 sentence0")
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
