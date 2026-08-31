package storage

import (
	"context"
	"testing"

	"github.com/WWaynee/content-hub/config"
	"github.com/WWaynee/content-hub/storage/model"
)

// TestExpandScopeToFileIDs 验证勾选目录递归展开为文件 ID。
func TestExpandScopeToFileIDs(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("配置加载失败: %v", err)
	}
	if _, err := InitMySQL(&cfg.MySQL); err != nil {
		t.Skipf("MySQL 不可用: %v", err)
	}
	ctx := context.Background()
	tenantID := uint64(99992000)

	// 建目录层级：root -> dirA -> dirB
	dirA := &model.KbaseDir{TenantID: tenantID, Scope: ScopePublic, ParentID: 0, Name: "A"}
	CreateDir(ctx, dirA)
	dirB := &model.KbaseDir{TenantID: tenantID, Scope: ScopePublic, ParentID: dirA.ID, Name: "B"}
	CreateDir(ctx, dirB)

	// 建文件：dirA 下 f1，dirB 下 f2
	f1 := &model.KbaseFile{TenantID: tenantID, Scope: ScopePublic, DirID: dirA.ID, Name: "f1.md", FileType: "md", Active: 1}
	CreateFile(ctx, f1)
	f2 := &model.KbaseFile{TenantID: tenantID, Scope: ScopePublic, DirID: dirB.ID, Name: "f2.md", FileType: "md", Active: 1}
	CreateFile(ctx, f2)

	// 勾选 dirA，递归展开应含 f1（直属）+ f2（子目录）
	scopes := []model.RequirementScope{{ScopeType: ScopePublic, TargetType: "dir", DirID: dirA.ID}}
	ids, err := ExpandScopeToFileIDs(ctx, tenantID, scopes, ScopePublic)
	if err != nil {
		t.Fatalf("ExpandScopeToFileIDs 失败: %v", err)
	}
	set := map[uint64]bool{}
	for _, id := range ids {
		set[id] = true
	}
	if !set[f1.ID] {
		t.Errorf("应包含直属文件 f1(id=%d)", f1.ID)
	}
	if !set[f2.ID] {
		t.Errorf("应包含子目录文件 f2(id=%d)", f2.ID)
	}

	// 清理
	db := GetDB()
	db.Exec("DELETE FROM kbase_files WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM kbase_dirs WHERE tenant_id = ?", tenantID)
}
