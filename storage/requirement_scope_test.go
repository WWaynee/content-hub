package storage

import (
	"context"
	"testing"

	"github.com/WWaynee/content-hub/config"
	"github.com/WWaynee/content-hub/storage/model"
)

// TestSetRequirementScopes 验证范围设置的"先删后插"。
func TestSetRequirementScopes(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("配置加载失败: %v", err)
	}
	if _, err := InitMySQL(&cfg.MySQL); err != nil {
		t.Skipf("MySQL 不可用: %v", err)
	}
	ctx := context.Background()
	tenantID := uint64(99991000)

	// 先创建一个需求单（需要 workspace），简化直接插 requirement
	req := &model.Requirement{WorkspaceID: 1, TenantID: tenantID, Title: "t", Version: 1}
	GetDB().Create(req)

	scopes := []model.RequirementScope{
		{ScopeType: "public", TargetType: "dir", DirID: 10},
		{ScopeType: "private", TargetType: "file", FileID: 20},
	}
	if err := SetRequirementScopes(ctx, req.ID, tenantID, scopes); err != nil {
		t.Fatalf("SetRequirementScopes 失败: %v", err)
	}
	list, err := ListRequirementScopes(ctx, req.ID)
	if err != nil {
		t.Fatalf("ListRequirementScopes 失败: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("应保存 2 条范围，实际 %d", len(list))
	}

	// 再设置 1 条，应替换为 1 条
	if err := SetRequirementScopes(ctx, req.ID, tenantID, []model.RequirementScope{{ScopeType: "public", TargetType: "file", FileID: 30}}); err != nil {
		t.Fatalf("二次设置失败: %v", err)
	}
	list, _ = ListRequirementScopes(ctx, req.ID)
	if len(list) != 1 {
		t.Fatalf("二次设置应替换为 1 条，实际 %d", len(list))
	}

	// 清理
	GetDB().Exec("DELETE FROM requirement_scope WHERE requirement_id = ?", req.ID)
	GetDB().Exec("DELETE FROM requirements WHERE id = ?", req.ID)
}
