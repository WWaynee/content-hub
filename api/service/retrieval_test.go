//go:build integration

package service

import (
	"context"
	"testing"

	"github.com/WWaynee/content-hub/config"
	"github.com/WWaynee/content-hub/storage"
)

// TestRetrievalBatchLifecycle 验证：检索→句子级展开→落快照→惰性失效判定。
func TestRetrievalBatchLifecycle(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("配置加载失败: %v", err)
	}
	if cfg.OSS.AccessKeyID == "" || cfg.Embedding.APIKey == "" {
		t.Skip("OSS/Embedding 未配置，跳过")
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
	tenantID := uint64(99990020)

	// 1. 上传文档
	content := "# 放假通知\n\n## 假期安排\n2026年春节假期为2月15日至2月23日，共9天。\n\n## 值班要求\n各科室安排值班人员在岗。"
	_, err = IngestAndParse(ctx, IngestParams{
		TenantID: tenantID, Scope: storage.ScopePrivate, OwnerUserID: 1, DirID: 0,
		FileName: "放假通知.md", Content: []byte(content),
	})
	if err != nil {
		t.Fatalf("IngestAndParse 失败: %v", err)
	}

	// 2. 创建工作区 + 需求单
	w, err := CreateWorkspace(ctx, tenantID, 1, "测试")
	if err != nil {
		t.Fatalf("CreateWorkspace 失败: %v", err)
	}
	req, _ := storage.GetRequirementByWorkspace(ctx, tenantID, w.ID)

	// 3. 检索（句子级展开）
	hits, err := SearchKbaseSentences(ctx, tenantID, "春节假期安排")
	if err != nil {
		t.Fatalf("SearchKbaseSentences 失败: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("应检回到至少 1 个句子级证据")
	}
	t.Logf("句子级证据 %d 条", len(hits))

	// 4. 落检索快照
	batchID, err := PersistRetrievalBatch(ctx, tenantID, w.ID, req.ID, req.Version, []string{"春节假期"}, hits)
	if err != nil {
		t.Fatalf("PersistRetrievalBatch 失败: %v", err)
	}
	if batchID == 0 {
		t.Fatal("应返回 batchID")
	}

	// 5. 判定：当前 version 一致，应不过期
	stale, err := IsBatchStale(ctx, batchID, req.ID)
	if err != nil {
		t.Fatalf("IsBatchStale 失败: %v", err)
	}
	if stale {
		t.Fatal("version 未变，batch 不应过期")
	}

	// 6. 改需求单 → version+1 → 应过期
	if _, err := UpdateRequirementField(ctx, req.ID, "style_tone", "正式"); err != nil {
		t.Fatalf("UpdateRequirementField 失败: %v", err)
	}
	stale, err = IsBatchStale(ctx, batchID, req.ID)
	if err != nil {
		t.Fatalf("IsBatchStale 失败: %v", err)
	}
	if !stale {
		t.Fatal("version 已变，batch 应判定为过期")
	}

	// 清理
	cleanupRetrieval(ctx, tenantID, w.ID)
}

func cleanupRetrieval(ctx context.Context, tenantID, workspaceID uint64) {
	db := storage.GetDB()
	db.Exec("DELETE FROM retrieval_batch_items WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM retrieval_batches WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM requirements WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM workspaces WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM doc_sentences WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM doc_chunks WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM doc_versions WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM kbase_files WHERE tenant_id = ?", tenantID)
}
