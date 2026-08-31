//go:build integration

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/WWaynee/content-hub/config"
	"github.com/WWaynee/content-hub/storage"
	"github.com/WWaynee/content-hub/storage/model"
)

// TestMultitenantIsolation 数据层多租户隔离对抗：
// 租户 A 的私有数据，租户 B 无法读取/列出/越权访问。
func TestMultitenantIsolation(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("配置加载失败: %v", err)
	}
	if _, err := storage.InitMySQL(&cfg.MySQL); err != nil {
		t.Skipf("MySQL 不可用: %v", err)
	}
	ctx := context.Background()
	tenantA := uint64(99993001)
	tenantB := uint64(99993002)

	// 1. 租户 A 创建私有目录 + 文件
	dirA := &model.KbaseDir{TenantID: tenantA, Scope: storage.ScopePrivate, OwnerUserID: 1, Name: "A私密目录"}
	if err := storage.CreateDir(ctx, dirA); err != nil {
		t.Fatalf("A 建目录失败: %v", err)
	}
	fileA := &model.KbaseFile{TenantID: tenantA, Scope: storage.ScopePrivate, DirID: dirA.ID, OwnerUserID: 1,
		Name: "a.md", FileType: "md", Active: 1}
	if err := storage.CreateFile(ctx, fileA); err != nil {
		t.Fatalf("A 建文件失败: %v", err)
	}
	// 工作区 + 需求单（A 私有）
	wA, _ := CreateWorkspace(ctx, tenantA, 1, "A私密工作区", nil)

	// 2. 租户 B 列私有库目录（自己名下）→ 不应看到 A 的目录
	bDirs, err := storage.ListDirs(ctx, tenantB, storage.ScopePrivate, 1, 0)
	if err != nil {
		t.Fatalf("B 列目录失败: %v", err)
	}
	for _, d := range bDirs {
		if d.ID == dirA.ID {
			t.Fatalf("租户 B 不应看到租户 A 的私有目录")
		}
	}

	// 3. B 用 A 的文件 id 查文件 → 应查不到（gorm.ErrRecordNotFound 或隔离）
	if _, err := storage.GetFileByID(ctx, tenantB, fileA.ID); err == nil {
		t.Fatalf("租户 B 越权访问租户 A 的文件应失败")
	}

	// 4. B 用 A 的工作区 id 查工作区 → 应隔离
	if w, err := storage.GetWorkspaceByID(ctx, tenantB, 1, wA.ID); err == nil && w.ID != 0 {
		t.Fatalf("租户 B 越权访问租户 A 的工作区应失败，实际拿到 %d", w.ID)
	}

	// 5. B 读 A 的需求单 → 应查不到
	if _, err := storage.GetRequirementByWorkspace(ctx, tenantB, wA.ID); err == nil {
		t.Fatalf("租户 B 越权读租户 A 的需求单应失败")
	}

	// 6. B 列 A 目录下的文件 → 应为空
	aFilesAsB, err := storage.ListFilesByDir(ctx, tenantB, dirA.ID)
	if err != nil {
		t.Fatalf("B 列文件失败: %v", err)
	}
	if len(aFilesAsB) != 0 {
		t.Fatalf("租户 B 不应看到租户 A 目录下的文件")
	}

	t.Log("多租户隔离对抗验证通过：目录/文件/工作区/需求单 全部隔离")
	_ = errors.Is // 保持 import
	_ = fileA

	// 清理
	db := storage.GetDB()
	db.Exec("DELETE FROM requirements WHERE tenant_id IN (?,?)", tenantA, tenantB)
	db.Exec("DELETE FROM workspaces WHERE tenant_id IN (?,?)", tenantA, tenantB)
	db.Exec("DELETE FROM kbase_files WHERE tenant_id IN (?,?)", tenantA, tenantB)
	db.Exec("DELETE FROM kbase_dirs WHERE tenant_id IN (?,?)", tenantA, tenantB)
}
