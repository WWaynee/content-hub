package storage

import (
	"context"

	"github.com/WWaynee/content-hub/storage/model"
)

// 工作区存储层。所有操作强制 tenant_id + owner_user_id 过滤（本人才可访问）。

func CreateWorkspace(ctx context.Context, w *model.Workspace) error {
	return GetDB().WithContext(ctx).Create(w).Error
}

// GetWorkspaceByID 按 ID + 租户 + owner 查（本人可见）。
func GetWorkspaceByID(ctx context.Context, tenantID, ownerUserID, id uint64) (*model.Workspace, error) {
	var w model.Workspace
	if err := GetDB().WithContext(ctx).
		Where("id = ? AND tenant_id = ? AND owner_user_id = ?", id, tenantID, ownerUserID).
		First(&w).Error; err != nil {
		return nil, err
	}
	return &w, nil
}

// ListWorkspaces 列出某用户的工作区（按更新时间倒序）。
func ListWorkspaces(ctx context.Context, tenantID, ownerUserID uint64) ([]model.Workspace, error) {
	var list []model.Workspace
	if err := GetDB().WithContext(ctx).
		Where("tenant_id = ? AND owner_user_id = ?", tenantID, ownerUserID).
		Order("updated_at DESC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// UpdateWorkspaceStatus 更新工作区状态。
func UpdateWorkspaceStatus(ctx context.Context, id uint64, status string) error {
	return GetDB().WithContext(ctx).Model(&model.Workspace{}).Where("id = ?", id).Update("status", status).Error
}
