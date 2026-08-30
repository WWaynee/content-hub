package storage

import (
	"errors"

	"gorm.io/gorm"

	"github.com/WWaynee/content-hub/storage/model"
)

// CreateTenant 创建租户。
func CreateTenant(tx *gorm.DB, t *model.Tenant) error {
	return tx.Create(t).Error
}

// GetTenantByID 按 ID 查租户（含软删过滤）。
func GetTenantByID(id uint64) (*model.Tenant, error) {
	var t model.Tenant
	if err := GetDB().Where("id = ?", id).First(&t).Error; err != nil {
		return nil, err
	}
	return &t, nil
}

// GetTenantByName 按名称查租户。
func GetTenantByName(name string) (*model.Tenant, error) {
	var t model.Tenant
	if err := GetDB().Where("name = ?", name).First(&t).Error; err != nil {
		return nil, err
	}
	return &t, nil
}

// IsTenantNameExists 判断租户名是否已存在。
func IsTenantNameExists(name string) (bool, error) {
	var cnt int64
	if err := GetDB().Model(&model.Tenant{}).Where("name = ?", name).Count(&cnt).Error; err != nil {
		return false, err
	}
	return cnt > 0, nil
}

// EnsureNotFound 判断是否为记录不存在的错误。
func EnsureNotFound(err error) bool { return errors.Is(err, gorm.ErrRecordNotFound) }
