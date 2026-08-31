package storage

import (
	"context"

	"github.com/WWaynee/content-hub/storage/model"
)

// 知识库目录递归展开（用于把勾选范围展开成文件 ID 列表锁定检索范围）。

// collectDirFileIDs 递归收集某目录（含其所有子目录）下的 active 文件 ID。
func collectDirFileIDs(ctx context.Context, tenantID, dirID uint64, acc *map[uint64]bool) error {
	// 该目录下的文件
	files, err := ListFilesByDir(ctx, tenantID, dirID)
	if err != nil {
		return err
	}
	for _, f := range files {
		(*acc)[f.ID] = true
	}
	// 该目录下的子目录（递归）
	subDirs, err := ListDirs(ctx, tenantID, ScopePublic, 0, dirID)
	if err == nil {
		for _, d := range subDirs {
			if err := collectDirFileIDs(ctx, tenantID, d.ID, acc); err != nil {
				return err
			}
		}
	}
	// 公有库 root 目录下也含私有？（scope 隔离，此处只展开同 scope；调用方保证 scope 一致）
	return nil
}

// ExpandScopeToFileIDs 把需求单勾选范围（dir/file 指针）递归展开为文件 ID 列表。
// scope 参数用于限定目录递归时的 scope（public/private）。
func ExpandScopeToFileIDs(ctx context.Context, tenantID uint64, scopes []model.RequirementScope, scope string) ([]uint64, error) {
	collected := map[uint64]bool{}
	for _, s := range scopes {
		if s.ScopeType != scope {
			continue
		}
		switch s.TargetType {
		case "file":
			if s.FileID != 0 {
				collected[s.FileID] = true
			}
		case "dir":
			if s.DirID != 0 {
				if err := collectDirFileIDs(ctx, tenantID, s.DirID, &collected); err != nil {
					return nil, err
				}
			}
		}
	}
	ids := make([]uint64, 0, len(collected))
	for id := range collected {
		ids = append(ids, id)
	}
	return ids, nil
}
