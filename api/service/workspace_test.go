package service

import (
	"context"
	"testing"

	"github.com/WWaynee/content-hub/config"
	"github.com/WWaynee/content-hub/storage"
)

func initWorkspace(t *testing.T) {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("配置加载失败: %v", err)
	}
	if _, err := storage.InitMySQL(&cfg.MySQL); err != nil {
		t.Skipf("MySQL 不可用: %v", err)
	}
}

// TestCreateWorkspaceAndUpdateRequirement 验证工作区+需求单创建，及 version 递增。
func TestCreateWorkspaceAndUpdateRequirement(t *testing.T) {
	initWorkspace(t)
	ctx := context.Background()
	tenantID := uint64(99990010) // 独立测试租户

	w, err := CreateWorkspace(ctx, tenantID, 1, "测试工作区", nil)
	if err != nil {
		t.Fatalf("CreateWorkspace 失败: %v", err)
	}
	if w.ID == 0 {
		t.Fatal("应返回工作区 ID")
	}

	req, err := storage.GetRequirementByWorkspace(ctx, tenantID, w.ID)
	if err != nil {
		t.Fatalf("查需求单失败: %v", err)
	}
	if req.Version != 1 {
		t.Fatalf("初始 version 应为 1，实际=%d", req.Version)
	}

	// 更新字段 → version 应 +1
	updated, err := UpdateRequirementField(ctx, req.ID, "style_tone", "正式")
	if err != nil {
		t.Fatalf("UpdateRequirementField 失败: %v", err)
	}
	if updated.Version != 2 {
		t.Fatalf("更新后 version 应为 2，实际=%d", updated.Version)
	}
	if updated.StyleTone != "正式" {
		t.Fatalf("style_tone 应更新为正式，实际=%q", updated.StyleTone)
	}

	// 清理
	db := storage.GetDB()
	db.Exec("DELETE FROM requirements WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM workspaces WHERE tenant_id = ?", tenantID)
}
