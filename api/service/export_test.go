package service

import (
	"context"
	"strings"
	"testing"

	"github.com/WWaynee/content-hub/agent"
	"github.com/WWaynee/content-hub/config"
	"github.com/WWaynee/content-hub/storage"
	"github.com/WWaynee/content-hub/storage/model"
)

// TestExportArticle 验证导出合并 md（正文 + 证据清单）。
func TestExportArticle(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("配置加载失败: %v", err)
	}
	if _, err := storage.InitMySQL(&cfg.MySQL); err != nil {
		t.Skipf("MySQL 不可用: %v", err)
	}
	ctx := context.Background()
	tenantID := uint64(99990050)

	// 先造一个 doc_sentence（供证据回查）
	docSent := &model.DocSentence{
		TenantID: tenantID, FileID: 100, VersionMd5: "v1", ChunkID: 1,
		SentenceIndex: 0, Content: "原文句子内容。",
	}
	storage.GetDB().Create(docSent)

	// 用 generation 落库造一个快照
	w, _ := CreateWorkspace(ctx, tenantID, 1, "导出测试", nil)
	evidence := []agent.Evidence{{FileID: 100, DocSentenceID: docSent.ID, VersionMd5: "v1", SourceText: "原文句子内容。"}}
	article := &agent.Article{Title: "导出稿", Sections: []agent.Section{{
		Heading: "第一章", Paragraphs: []agent.Paragraph{{
			Sentences: []agent.Sentence{{Text: "引用句。", EvidenceRefs: []uint64{0}}},
		}},
	}}}
	verID, err := PersistArticleSnapshot(ctx, tenantID, w.ID, 1, article, evidence)
	if err != nil {
		t.Fatalf("落快照失败: %v", err)
	}

	// 导出
	md, err := ExportArticle(ctx, verID)
	if err != nil {
		t.Fatalf("ExportArticle 失败: %v", err)
	}
	// 正文在前、证据清单在后
	if !strings.Contains(md, "导出稿") {
		t.Errorf("导出应含正文标题")
	}
	if !strings.Contains(md, "证据清单") {
		t.Errorf("导出应含证据清单标题")
	}
	if !strings.Contains(md, "原文句子内容。") {
		t.Errorf("证据清单应含原文")
	}
	if !strings.Contains(md, "引用句。") {
		t.Errorf("证据清单应含稿件句子")
	}
	t.Logf("导出内容：\n%s", md)

	// 清理
	db := storage.GetDB()
	db.Exec("DELETE FROM evidence_bindings WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM article_sentences WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM article_versions WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM articles WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM doc_sentences WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM requirements WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM workspaces WHERE tenant_id = ?", tenantID)
}
