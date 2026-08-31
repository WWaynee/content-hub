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

// WorkspaceWithRequirement 工作区 + 关联需求单（用于列表返回展示字段）。
type WorkspaceWithRequirement struct {
	model.Workspace
	RequirementTitle string
	RequirementTags  string
	RequirementPlatforms string
	RequirementStatus string
}

// ListWorkspacesFiltered 列出某用户的工作区，支持 title 子串 + status 精确 + tag/platform JSON contains 过滤。
// 返回工作区 + 关联需求单的展示字段（不 join 复杂，采用 left join + 选中字段）。
func ListWorkspacesFiltered(ctx context.Context, tenantID, ownerUserID uint64, titleKeyword, status, tag, platform string) ([]WorkspaceWithRequirement, error) {
	q := GetDB().WithContext(ctx).
		Table("workspaces AS w").
		Select(`w.*, r.title AS requirement_title, r.tags AS requirement_tags,
		        r.platforms AS requirement_platforms, w.status AS requirement_status`).
		Joins("LEFT JOIN requirements AS r ON r.workspace_id = w.id").
		Where("w.tenant_id = ? AND w.owner_user_id = ? AND w.deleted_at IS NULL", tenantID, ownerUserID)

	if titleKeyword != "" {
		q = q.Where("w.title LIKE ?", "%"+titleKeyword+"%")
	}
	if status != "" {
		q = q.Where("w.status = ?", status)
	}
	if tag != "" {
		q = q.Where("JSON_CONTAINS(r.tags, ?)", `"`+tag+`"`)
	}
	if platform != "" {
		q = q.Where("JSON_CONTAINS(r.platforms, ?)", `"`+platform+`"`)
	}

	var list []WorkspaceWithRequirement
	if err := q.Order("w.updated_at DESC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// SoftDeleteWorkspace 软删除工作区（仅本人）。
func SoftDeleteWorkspace(ctx context.Context, tenantID, ownerUserID, id uint64) error {
	return GetDB().WithContext(ctx).
		Where("id = ? AND tenant_id = ? AND owner_user_id = ?", id, tenantID, ownerUserID).
		Delete(&model.Workspace{}).Error
}
