package storage

import (
	"errors"

	"gorm.io/gorm"

	"github.com/WWaynee/content-hub/storage/model"
)

// CreateUser 创建用户。
func CreateUser(tx *gorm.DB, u *model.User) error {
	return tx.Create(u).Error
}

// GetUserByUsername 按租户+用户名查用户。
func GetUserByUsername(tenantID uint64, username string) (*model.User, error) {
	var u model.User
	if err := GetDB().Where("tenant_id = ? AND username = ?", tenantID, username).First(&u).Error; err != nil {
		return nil, err
	}
	return &u, nil
}

// GetUserByUsernameGlobal 按用户名（全局唯一）查用户。用于不传租户ID的登录。
func GetUserByUsernameGlobal(username string) (*model.User, error) {
	var u model.User
	if err := GetDB().Where("username = ?", username).First(&u).Error; err != nil {
		return nil, err
	}
	return &u, nil
}

// IsUsernameExists 判断指定租户下用户名是否已存在。
func IsUsernameExists(tenantID uint64, username string) (bool, error) {
	var cnt int64
	if err := GetDB().Model(&model.User{}).Where("tenant_id = ? AND username = ?", tenantID, username).Count(&cnt).Error; err != nil {
		return false, err
	}
	return cnt > 0, nil
}

// IsUsernameExistsGlobal 判断用户名是否已被任何租户占用（全局唯一）。
func IsUsernameExistsGlobal(username string) (bool, error) {
	var cnt int64
	if err := GetDB().Model(&model.User{}).Where("username = ?", username).Count(&cnt).Error; err != nil {
		return false, err
	}
	return cnt > 0, nil
}

// Role 角色常量
const (
	RoleAdmin  = "admin"
	RoleMember = "member"
)

// EnsureNotFoundUser ...
func EnsureNotFoundUser(err error) bool { return errors.Is(err, gorm.ErrRecordNotFound) }
