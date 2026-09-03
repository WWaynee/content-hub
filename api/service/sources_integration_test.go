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

// P04 证据"人读 source"装配验收（RFC rev-2 §8.2-Q2/§10.1、rev-4 §13.6/W6）。
//
// 通过真实 MySQL 落一份快照：bound 句引用不同来源文档句，unbound 句无引用。
// 断言 BuildSentenceViews/LoadSentenceSources 能把每个 doc_sentence 还原成
// {source_text/文档名/章节/version_md5/has_newer/file_deleted}，has_newer/file_deleted 按
// kbase_files.current_version_md5 与 active 差异给出。ExportArticle 的证据清单含可复制原句与文件名。

func TestEvidenceSourcesAssembly(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("配置加载失败: %v", err)
	}
	if _, err := storage.InitMySQL(&cfg.MySQL); err != nil {
		t.Skipf("MySQL 不可用: %v", err)
	}
	ctx := context.Background()
	tenantID := uint64(99990681)

	// --- 三种来源文档 ---
	// f1：公库、current 等于被引版本(v1-now) → has_newer=false
	// f2：公库、current 已前进到 v2-cur，但被绑的是老版本 v1-old        → has_newer=true
	// f3：私有(被软删 active=0)，current==被引版本，name 仍应可还原     → file_deleted=true / has_newer=false
	f1 := model.KbaseFile{TenantID: tenantID, Scope: storage.ScopePublic, OwnerUserID: 0, Name: "放假通知.md", CurrentVersionMd5: "v1-now", Active: 1}
	f2 := model.KbaseFile{TenantID: tenantID, Scope: storage.ScopePublic, OwnerUserID: 0, Name: "年度总结.md", CurrentVersionMd5: "v2-cur", Active: 1}
	f3 := model.KbaseFile{TenantID: tenantID, Scope: storage.ScopePrivate, OwnerUserID: 1, Name: "内部纪要.md", CurrentVersionMd5: "f3v", Active: 1}
	db := storage.GetDB()
	if err := db.Create(&f1).Error; err != nil {
		t.Fatalf("create f1: %v", err)
	}
	if err := db.Create(&f2).Error; err != nil {
		t.Fatalf("create f2: %v", err)
	}
	if err := db.Create(&f3).Error; err != nil {
		t.Fatalf("create f3: %v", err)
	}
	// f3 模拟资料随后被删除：走业务置 active=0（而非 model 构造 Active:0，后者会被 GORM 的
	// column default:1 吞掉而落不进去）。删除后 doc_sentences/doc_versions 仍保留(不可变锚)，
	// 因此装配仍能还原文件名并给出 file_deleted 提示。
	if err := db.Model(&model.KbaseFile{}).Where("id = ?", f3.ID).Update("active", 0).Error; err != nil {
		t.Fatalf("deactivate f3: %v", err)
	}

	// doc_sentences + 其 chunk(章节)
	mkChunk := func(fid uint64, title string) *model.DocChunk {
		c := &model.DocChunk{TenantID: tenantID, FileID: fid, VersionMd5: "x", Content: "chunk"}
		if err := db.Create(c).Error; err != nil {
			t.Fatalf("create chunk: %v", err)
		}
		if title != "" {
			db.Model(c).Update("chapter_title", title)
		}
		return c
	}
	mkSent := func(fid uint64, md5, text string, chunkID uint64) *model.DocSentence {
		s := &model.DocSentence{TenantID: tenantID, FileID: fid, VersionMd5: md5, ChunkID: chunkID, Content: text}
		if err := db.Create(s).Error; err != nil {
			t.Fatalf("create sentence: %v", err)
		}
		return s
	}
	s1 := mkSent(f1.ID, "v1-now", "放假共9天。", mkChunk(f1.ID, "通知正文").ID)
	s2 := mkSent(f2.ID, "v1-old", "全年举办活动40场。", mkChunk(f2.ID, "活动综述").ID)
	s3 := mkSent(f3.ID, "f3v", "内部分工安排如下。", mkChunk(f3.ID, "").ID)

	// workspace + article
	w, werr := CreateWorkspace(ctx, tenantID, 1, "P04证据装配", nil)
	if werr != nil {
		t.Fatalf("create workspace: %v", werr)
	}

	// 快照：句0 = bound(f1) ；句1 = bound(f2 + f3 两条) ；句2 = unbound
	art := &agent.Article{
		Title: "P04稿",
		Sections: []agent.Section{{Paragraphs: []agent.Paragraph{{
			Sentences: []agent.Sentence{
				{Text: "放假九天。", EvidenceRefs: []uint64{0}},
				{Text: "全年活动四十场并完成内部分工。", EvidenceRefs: []uint64{1, 2}},
				{Text: "各部门要严格落实。", EvidenceRefs: []uint64{}},
			},
		}}}},
	}
	evidence := []agent.Evidence{
		{FileID: f1.ID, DocSentenceID: s1.ID, VersionMd5: s1.VersionMd5, SourceText: s1.Content},
		{FileID: f2.ID, DocSentenceID: s2.ID, VersionMd5: s2.VersionMd5, SourceText: s2.Content},
		{FileID: f3.ID, DocSentenceID: s3.ID, VersionMd5: s3.VersionMd5, SourceText: s3.Content},
	}
	verID, err := PersistArticleSnapshot(ctx, tenantID, w.ID, art, evidence)
	if err != nil {
		t.Fatalf("落快照失败: %v", err)
	}
	sents, _ := storage.ListArticleSentences(ctx, verID)
	binds, _ := storage.ListArticleBindings(ctx, verID)

	// --- 1. 装配 sources ---
	sourceBySent := LoadSentenceSources(ctx, tenantID, binds)
	views := BuildSentenceViews(sents, sourceBySent, ClaimStatusBySent(binds))
	if len(views) != 3 {
		t.Fatalf("应有 3 sentence_views，实得 %d", len(views))
	}

	// 句0 bound：单源，来自 f1，has_newer=false
	if views[0].ClaimType != ClaimTypeBound || len(views[0].Sources) != 1 {
		t.Fatalf("句0 应为 bound + 1 source，实得 claim=%s sources=%d", views[0].ClaimType, len(views[0].Sources))
	}
	src0 := views[0].Sources[0]
	if !strings.Contains(src0.SourceText, "放假共9天") || src0.FileName != "放假通知.md" ||
		src0.ChapterTitle != "通知正文" || src0.VersionMd5 != "v1-now" {
		t.Errorf("句0 source 人读字段不符: %+v", src0)
	}
	if src0.HasNewer || src0.FileDeleted {
		t.Errorf("句0 不应 has_newer/file_deleted: %+v", src0)
	}

	// 句1 bound：2 source（f2→has_newer、f3→file_deleted）
	if views[1].ClaimType != ClaimTypeBound || len(views[1].Sources) != 2 {
		t.Fatalf("句1 应为 bound + 2 source，实得 %d", len(views[1].Sources))
	}
	foundNewer := false
	foundDeleted := false
	for _, s := range views[1].Sources {
		switch {
		case s.FileID == f2.ID:
			foundNewer = s.HasNewer
			if s.FileName != "年度总结.md" || s.ChapterTitle != "活动综述" {
				t.Errorf("句1 f2 source 字段不符: %+v", s)
			}
		case s.FileID == f3.ID:
			foundDeleted = s.FileDeleted
			// 已删除文件仍能还原 name（不可变锚）
			if s.FileName != "内部纪要.md" {
				t.Errorf("句1 f3 已删除也应还原文件名: %+v", s)
			}
			if s.HasNewer {
				t.Errorf("句1 f3 current 未变不应 has_newer: %+v", s)
			}
		}
	}
	if !foundNewer || !foundDeleted {
		t.Errorf("句1 应识别 f2 has_newer(新版本) 与 f3 file_deleted(已删): newer=%v deleted=%v", foundNewer, foundDeleted)
	}

	// 句2 unbound → plausible-ai、sources 空
	if views[2].ClaimType != ClaimTypePlausibleAI || len(views[2].Sources) != 0 {
		t.Errorf("句2 应为 plausible-ai + 空 sources: %+v", views[2])
	}

	// --- 2. 导出证据清单含可复制原句 + 文件名；已删/有新版有提示 ---
	md, err := ExportArticle(ctx, verID)
	if err != nil {
		t.Fatalf("ExportArticle: %v", err)
	}
	for _, want := range []string{"放假通知.md", "年度总结.md", "内部纪要.md", "放假九天。", "已被删除", "新版本"} {
		if !strings.Contains(md, want) {
			t.Errorf("导出证据清单缺失 %q，md:\n%s", want, md)
		}
	}

	// --- 清理 ---
	db.Exec("DELETE FROM evidence_bindings WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM article_sentences WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM article_versions WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM articles WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM workspaces WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM requirements WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM doc_sentences WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM doc_chunks WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM kbase_files WHERE tenant_id = ?", tenantID)
}
