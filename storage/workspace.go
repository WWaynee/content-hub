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
	RequirementTitle    string `json:"requirement_title"`
	RequirementTags     string `json:"requirement_tags"`
	RequirementPlatforms string `json:"requirement_platforms"`
	RequirementStatus   string `json:"requirement_status"`
	RequirementWordCount int   `json:"requirement_word_count"`
	RequirementStyleTone   string `json:"requirement_style_tone"`
	RequirementStyleEmotion string `json:"requirement_style_emotion"`
	RequirementStyleAudience string `json:"requirement_style_audience"`
	RequirementStylePurpose string `json:"requirement_style_purpose"`
	RequirementStyleSubject string `json:"requirement_style_subject"`
	RequirementChapterRequirement string `json:"requirement_chapter_requirement"`
	RequirementVersion int   `json:"requirement_version"`
}

// ListWorkspacesFilters 工作区列表过滤参数。
type ListWorkspacesFilters struct {
	Title    string
	Statuses []string
	Tag      string
	Platform string
	Sort     string // "" | time_asc | time_desc
}

// ListWorkspacesFiltered 列出某用户的工作区（tenant + owner 强制隔离），支持：
// title 子串 + statuses 多值精确 + tag/platform JSON contains + Sort 时间排序（asc/desc）。
// 不启用分页：返回当前用户的全量工作区。
func ListWorkspacesFiltered(ctx context.Context, tenantID, ownerUserID uint64, f ListWorkspacesFilters) ([]WorkspaceWithRequirement, error) {
	q := GetDB().WithContext(ctx).
		Table("workspaces AS w").
		Select(`w.*,
		        r.title AS requirement_title,
		        r.tags AS requirement_tags,
		        r.platforms AS requirement_platforms,
		        w.status AS requirement_status,
		        r.word_count AS requirement_word_count,
		        r.style_tone AS requirement_style_tone,
		        r.style_emotion AS requirement_style_emotion,
		        r.style_audience AS requirement_style_audience,
		        r.style_purpose AS requirement_style_purpose,
		        r.style_subject AS requirement_style_subject,
		        r.chapter_requirement AS requirement_chapter_requirement,
		        r.version AS requirement_version`).
		Joins("LEFT JOIN requirements AS r ON r.workspace_id = w.id").
		Where("w.tenant_id = ? AND w.owner_user_id = ? AND w.deleted_at IS NULL", tenantID, ownerUserID)

	if f.Title != "" {
		q = q.Where("w.title LIKE ?", "%"+f.Title+"%")
	}
	if len(f.Statuses) > 0 {
		q = q.Where("w.status IN ?", f.Statuses)
	}
	if f.Tag != "" {
		q = q.Where("JSON_CONTAINS(r.tags, ?)", `"`+f.Tag+`"`)
	}
	if f.Platform != "" {
		q = q.Where("JSON_CONTAINS(r.platforms, ?)", `"`+f.Platform+`"`)
	}

	switch f.Sort {
	case "time_asc":
		q = q.Order("w.updated_at ASC")
	default:
		q = q.Order("w.updated_at DESC")
	}

	var list []WorkspaceWithRequirement
	if err := q.Find(&list).Error; err != nil {
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
