package service

import (
	"context"

	"github.com/WWaynee/content-hub/storage"
)

// RequirementFileIDScope 把需求单勾选范围展开为文件 ID 列表（公有+私有合并，锁定检索范围）。
// 无勾选范围时返回 nil（表示不限范围，全租户检索）。
func RequirementFileIDScope(ctx context.Context, tenantID, requirementID uint64) ([]uint64, error) {
	scopes, err := storage.ListRequirementScopes(ctx, requirementID)
	if err != nil {
		return nil, err
	}
	if len(scopes) == 0 {
		return nil, nil // 未勾选范围 → 不限
	}

	var all []uint64
	seen := map[uint64]bool{}
	for _, s := range scopes {
		ids, err := storage.ExpandScopeToFileIDs(ctx, tenantID, scopes, s.ScopeType)
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			if !seen[id] {
				seen[id] = true
				all = append(all, id)
			}
		}
	}
	return all, nil
}
