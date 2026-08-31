//go:build integration

package service

import (
	"context"
	"testing"

	"github.com/WWaynee/content-hub/config"
	"github.com/WWaynee/content-hub/storage"
)

// TestEnsureBatchFreshLifecycle 验证惰性失效判定：
// ①无 batch 放行 ②版本一致放行 ③需求单变更后（version+1）拒绝局部修订。
func TestEnsureBatchFreshLifecycle(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("配置加载失败: %v", err)
	}
	if _, err := storage.InitMySQL(&cfg.MySQL); err != nil {
		t.Skipf("MySQL 不可用: %v", err)
	}
	ctx := context.Background()
	tenantID := uint64(99992040)

	// 创建工作区 + 需求单（version=1）
	w, _ := CreateWorkspace(ctx, tenantID, 1, "失效判定", nil)
	req, _ := storage.GetRequirementByWorkspace(ctx, tenantID, w.ID)
	if req.Version != 1 {
		t.Fatalf("初始 version 应为 1，实际 %d", req.Version)
	}

	// ① 无 batch → 放行
	if err := EnsureBatchFresh(ctx, w.ID, req.ID); err != nil {
		t.Fatalf("无 batch 时应放行，实际: %v", err)
	}

	// 落 batch（version=1）
	hits := []KbaseHit{{FileID: 1, DocSentenceID: 11, VersionMd5: "v1", ChunkID: 2}}
	batchID, err := PersistRetrievalBatch(ctx, tenantID, w.ID, req.ID, req.Version, []string{"q"}, hits)
	if err != nil {
		t.Fatalf("落 batch 失败: %v", err)
	}

	// ② 版本一致 → 放行
	if err := EnsureBatchFresh(ctx, w.ID, req.ID); err != nil {
		t.Fatalf("版本一致时应放行，实际: %v", err)
	}

	// ③ 改需求单 → version+1 → 拒绝
	if _, err := UpdateRequirementField(ctx, req.ID, "style_tone", "正式"); err != nil {
		t.Fatalf("更新需求单失败: %v", err)
	}
	if err := EnsureBatchFresh(ctx, w.ID, req.ID); err == nil {
		t.Fatal("需求单变更后局部修订应被拒绝（惰性失效）")
	} else {
		t.Logf("正确拒绝：%v", err)
	}

	_ = batchID // batch 落库验证见下方查表

	// 验证 batch 真的落库且 items 有记录
	b, _ := storage.GetLatestRetrievalBatch(ctx, w.ID)
	if b == nil || b.RequirementID != req.ID {
		t.Fatalf("batch 应落库且关联需求单")
	}
	ids, _ := storage.ListBatchSentenceIDs(ctx, b.ID)
	if len(ids) != 1 || ids[0] != 11 {
		t.Errorf("batch items 应为 doc_sentence_id=11，实际 %v", ids)
	}

	// 清理
	db := storage.GetDB()
	db.Exec("DELETE FROM retrieval_batch_items WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM retrieval_batches WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM requirements WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM workspaces WHERE tenant_id = ?", tenantID)
}
