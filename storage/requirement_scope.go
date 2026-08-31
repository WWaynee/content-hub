package storage

import (
	"context"

	"gorm.io/gorm"

	"github.com/WWaynee/content-hub/storage/model"
)

// 需求单引用范围存储层（活引用：存 dir/file 指针，检索时递归展开）。

// SetRequirementScopes 替换某需求单的全部引用范围（先删后插，事务）。
func SetRequirementScopes(ctx context.Context, requirementID, tenantID uint64, scopes []model.RequirementScope) error {
	return GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("requirement_id = ?", requirementID).Delete(&model.RequirementScope{}).Error; err != nil {
			return err
		}
		if len(scopes) == 0 {
			return nil
		}
		for i := range scopes {
			scopes[i].RequirementID = requirementID
			scopes[i].TenantID = tenantID
		}
		return tx.Create(&scopes).Error
	})
}

// ListRequirementScopes 列出某需求单的全部引用范围。
func ListRequirementScopes(ctx context.Context, requirementID uint64) ([]model.RequirementScope, error) {
	var list []model.RequirementScope
	if err := GetDB().WithContext(ctx).
		Where("requirement_id = ?", requirementID).
		Order("id ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}
