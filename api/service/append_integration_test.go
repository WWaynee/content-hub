//go:build integration

package service

import (
	"context"
	"testing"

	"github.com/WWaynee/content-hub/agent"
	"github.com/WWaynee/content-hub/config"
	"github.com/WWaynee/content-hub/observability"
	"github.com/WWaynee/content-hub/storage"
)

// TestAppendArticleContent 验证追加段落完整链路（LLM 生成 + 检索 + 落快照，现有句子继承）。
func TestAppendArticleContent(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("配置加载失败: %v", err)
	}
	if cfg.LLM.APIKey == "" || cfg.Embedding.APIKey == "" || cfg.OSS.AccessKeyID == "" {
		t.Skip("LLM/Embedding/OSS 未配置，跳过")
	}
	if _, err := storage.InitMySQL(&cfg.MySQL); err != nil {
		t.Skipf("MySQL 不可用: %v", err)
	}
	if storage.OSSClient == nil {
		_ = storage.InitOSS()
	}
	if storage.QdrantClient == nil {
		_ = storage.InitQdrant(4096)
	}
	ctx := context.Background()
	tenantID := uint64(99992030)
	// 全文以文档 owner=1 身份跑：IngestAndParse 建私库文档、AppendArticleContent 内部检索该私库补证都
	// 需在同一可见身份下才能命中（否则检索只见公库→0 命中→间歇性红，同 P01 字面 plane 根因）。
	ctx = observability.WithTenantUser(ctx, tenantID, 1)

	// 上传资料
	content := "# 放假通知\n\n假期为2月15日至2月23日，共9天。\n各科室安排值班人员在岗。"
	if _, err := IngestAndParse(ctx, IngestParams{
		TenantID: tenantID, Scope: storage.ScopePrivate, OwnerUserID: 1, DirID: 0,
		FileName: "放假通知.md", Content: []byte(content),
	}); err != nil {
		t.Fatalf("IngestAndParse 失败: %v", err)
	}

	// 落快照（1 句）
	w, _ := CreateWorkspace(ctx, tenantID, 1, "追加测试", nil)
	article := &agent.Article{Title: "稿", Sections: []agent.Section{{Paragraphs: []agent.Paragraph{{
		Sentences: []agent.Sentence{{Text: "假期共9天。", EvidenceRefs: []uint64{}}},
	}}}}}
	if _, err := PersistArticleSnapshot(ctx, tenantID, w.ID, article, nil); err != nil {
		t.Fatalf("落快照失败: %v", err)
	}

	// 追加一段
	newVerID, err := AppendArticleContent(ctx, tenantID, w.ID, "追加一段值班要求的说明")
	if err != nil {
		t.Fatalf("AppendArticleContent 失败: %v", err)
	}

	sents, _ := storage.ListArticleSentences(ctx, newVerID)
	if len(sents) != 2 {
		t.Fatalf("应 2 句（原1 + 追加1），实际 %d", len(sents))
	}
	if sents[0].Content != "假期共9天。" {
		t.Errorf("原有句子应原样，实际 %q", sents[0].Content)
	}
	if sents[1].Content == "" {
		t.Errorf("追加句应为空")
	}
	t.Logf("追加的内容：%q", sents[1].Content)

	// 清理
	db := storage.GetDB()
	db.Exec("DELETE FROM evidence_bindings WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM article_sentences WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM article_versions WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM articles WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM doc_sentences WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM doc_chunks WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM doc_versions WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM kbase_files WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM requirements WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM workspaces WHERE tenant_id = ?", tenantID)
}
