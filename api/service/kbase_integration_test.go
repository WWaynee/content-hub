//go:build integration

package service

import (
	"context"
	"testing"

	"github.com/WWaynee/content-hub/config"
	"github.com/WWaynee/content-hub/observability"
	"github.com/WWaynee/content-hub/splitter"
	"github.com/WWaynee/content-hub/storage"
)

// 集成测试：真实链路（OSS + embedding + Qdrant + MySQL）。
// 依赖真实 .env（OSS/硅基流动 key），未配置或不可用时跳过。

func initKbase(t *testing.T) {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("跳过：配置加载失败 %v", err)
	}
	if cfg.OSS.AccessKeyID == "" || cfg.Embedding.APIKey == "" {
		t.Skip("跳过：OSS 或 Embedding 未配置真实 key")
	}
	if storage.OSSClient == nil {
		if err := storage.InitOSS(); err != nil {
			t.Skipf("跳过：OSS 不可用 %v", err)
		}
	}
	if storage.QdrantClient == nil {
		if err := storage.InitQdrant(4096); err != nil {
			t.Skipf("跳过：Qdrant 不可用 %v", err)
		}
	}
	if _, err := storage.InitMySQL(&cfg.MySQL); err != nil {
		t.Skipf("跳过：MySQL 不可用 %v", err)
	}
}

// TestSplitterSentences 补充：验证句子切分导出函数正常（单元级）。
func TestSplitterSentences(t *testing.T) {
	sents := splitter.Sentences("第一句。第二句！第三句？")
	if len(sents) != 3 {
		t.Fatalf("应切出 3 句，实际 %d: %v", len(sents), sents)
	}
}

// TestIngestAndSearch 真实端到端：上传一个 md 文档 → 全链路解析 → 检索命中。
// 由于涉及外部网络与费用，用短文档控制成本；不可用则跳过。
func TestIngestAndSearch(t *testing.T) {
	initKbase(t)
	ctx := context.Background()

	content := `# 招生政策

## 第一章 报名条件
本年度招生对象为应届高中毕业生。报名截止日期为 6 月 30 日。

## 第二章 录取规则
按高考成绩从高到低依次录取。`
	name := "招生政策.md"

	tenantID := uint64(99990001) // 独立测试租户，避免污染真实数据

	res, err := IngestAndParse(ctx, IngestParams{
		TenantID:    tenantID,
		Scope:       storage.ScopePrivate,
		OwnerUserID: 1,
		DirID:       0,
		FileName:    name,
		Content:     []byte(content),
	})
	if err != nil {
		t.Fatalf("IngestAndParse 失败: %v", err)
	}
	if res.FileID == 0 || res.VersionID == 0 {
		t.Fatalf("应返回 fileID/versionID")
	}

	// 检索：此文档为私库(owner=1)内容，须以该 owner 身份检索才能在可见平面内命中（P01）。
	sctx := observability.WithTenantUser(ctx, tenantID, 1)
	evs, err := SearchKbase(sctx, tenantID, "报名条件是什么")
	if err != nil {
		t.Fatalf("SearchKbase 失败: %v", err)
	}
	if len(evs) == 0 {
		t.Fatal("检索应命中至少 1 条证据")
	}
	t.Logf("检索命中 %d 条，top1 来源章节=%q", len(evs), evs[0].ChapterTitle)

	// 验证版本在库中 latest=1
	latest, err := storage.GetLatestVersion(ctx, res.FileID)
	if err != nil {
		t.Fatalf("GetLatestVersion 失败: %v", err)
	}
	if latest.VersionMd5 != res.VersionMd5 {
		t.Fatalf("latest 版本不符: want=%s got=%s", res.VersionMd5, latest.VersionMd5)
	}

	// 清理（避免重复跑污染）
	cleanupKbase(ctx, tenantID, res.FileID)
}

func cleanupKbase(ctx context.Context, tenantID, fileID uint64) {
	db := storage.GetDB()
	db.Exec("DELETE FROM doc_sentences WHERE file_id = ?", fileID)
	db.Exec("DELETE FROM doc_chunks WHERE file_id = ?", fileID)
	db.Exec("DELETE FROM doc_versions WHERE file_id = ?", fileID)
	db.Exec("DELETE FROM kbase_files WHERE id = ?", fileID)
}
