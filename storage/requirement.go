package storage

import (
	"context"

	"gorm.io/gorm"

	"github.com/WWaynee/content-hub/storage/model"
)

// 需求单存储层。version 字段驱动惰性失效。

func CreateRequirement(ctx context.Context, r *model.Requirement) error {
	if r.Version == 0 {
		r.Version = 1
	}
	return GetDB().WithContext(ctx).Create(r).Error
}

func GetRequirementByWorkspace(ctx context.Context, tenantID, workspaceID uint64) (*model.Requirement, error) {
	var r model.Requirement
	if err := GetDB().WithContext(ctx).
		Where("tenant_id = ? AND workspace_id = ?", tenantID, workspaceID).
		First(&r).Error; err != nil {
		return nil, err
	}
	return &r, nil
}

// UpdateRequirement 更新需求单字段，并递增 version（每次变更 +1）。
// fields 是允许更新的字段映射（已由上层白名单保证安全性）。
func UpdateRequirement(ctx context.Context, requirementID uint64, fields map[string]interface{}) error {
	return GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		fields["version"] = gorm.Expr("version + 1")
		return tx.Model(&model.Requirement{}).Where("id = ?", requirementID).Updates(fields).Error
	})
}

// BumpRequirementVersion 仅递增需求单 version（用于范围/核心字段手动变更后触发失效）。
func BumpRequirementVersion(ctx context.Context, requirementID uint64) error {
	return GetDB().WithContext(ctx).Model(&model.Requirement{}).
		Where("id = ?", requirementID).
		Update("version", gorm.Expr("version + 1")).Error
}

// GetRequirementByID 按 ID 查需求单。
func GetRequirementByID(ctx context.Context, id uint64) (*model.Requirement, error) {
	var r model.Requirement
	if err := GetDB().WithContext(ctx).Where("id = ?", id).First(&r).Error; err != nil {
		return nil, err
	}
	return &r, nil
}
