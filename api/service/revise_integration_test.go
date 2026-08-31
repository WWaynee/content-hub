//go:build integration

package service

import (
	"context"
	"strings"
	"testing"

	"github.com/WWaynee/content-hub/agent"
	"github.com/WWaynee/content-hub/config"
	"github.com/WWaynee/content-hub/storage"
)

// TestReviseSentenceFull 验证句子级修订完整链路（LLM 重写 + 重检测证据 + 落新快照）。
func TestReviseSentenceFull(t *testing.T) {
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
	tenantID := uint64(99992020)

	// 上传资料（同步解析，供检索）
	content := "# 放假通知\n\n假期为2月15日至2月23日，共9天。\n各科室安排值班人员在岗。"
	_, err = IngestAndParse(ctx, IngestParams{
		TenantID: tenantID, Scope: storage.ScopePrivate, OwnerUserID: 1, DirID: 0,
		FileName: "放假通知.md", Content: []byte(content),
	})
	if err != nil {
		t.Fatalf("IngestAndParse 失败: %v", err)
	}

	// 创建快照（2 句，句0绑证据、句1无证据）
	w, _ := CreateWorkspace(ctx, tenantID, 1, "修订端到端")
	evidence := []agent.Evidence{{FileID: 1, DocSentenceID: 1, VersionMd5: "v1", SourceText: "假期"}}
	article := &agent.Article{Title: "稿", Sections: []agent.Section{{Paragraphs: []agent.Paragraph{{
		Sentences: []agent.Sentence{
			{Text: "假期共9天。", EvidenceRefs: []uint64{0}},
			{Text: "请各单位做好值班安排。", EvidenceRefs: []uint64{}},
		},
	}}}}}
	if _, err := PersistArticleSnapshot(ctx, tenantID, w.ID, 1, article, evidence); err != nil {
		t.Fatalf("落快照失败: %v", err)
	}

	// 修订第 1 句（原"请各单位做好值班安排。"）
	newVerID, err := ReviseSentenceFull(ctx, tenantID, w.ID, 1, "改成更正式的语气")
	if err != nil {
		t.Fatalf("ReviseSentenceFull 失败: %v", err)
	}

	// 验证新快照：句0 不变、句1 文本变了
	sents, _ := storage.ListArticleSentences(ctx, newVerID)
	if len(sents) != 2 {
		t.Fatalf("应 2 句，实际 %d", len(sents))
	}
	if sents[0].Content != "假期共9天。" {
		t.Errorf("未动句0 应原样，实际 %q", sents[0].Content)
	}
	if strings.TrimSpace(sents[1].Content) == "请各单位做好值班安排。" {
		t.Errorf("被改句1 应已变化，实际仍为原文本 %q", sents[1].Content)
	}
	t.Logf("句1 修订后：%q", sents[1].Content)

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
