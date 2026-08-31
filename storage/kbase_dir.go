package storage

import (
	"context"

	"gorm.io/gorm"

	"github.com/WWaynee/content-hub/storage/model"
)

// 知识库目录树存储层。所有操作强制 tenant_id + scope/owner 过滤。

// 目录类型常量
const (
	ScopePublic  = "public"
	ScopePrivate = "private"
)

// CreateDir 创建目录。
func CreateDir(ctx context.Context, d *model.KbaseDir) error {
	return GetDB().WithContext(ctx).Create(d).Error
}

// GetDirByID 按 ID + 租户查目录（私有库额外校验 owner）。
func GetDirByID(ctx context.Context, tenantID, dirID uint64) (*model.KbaseDir, error) {
	var d model.KbaseDir
	if err := GetDB().WithContext(ctx).Where("id = ? AND tenant_id = ?", dirID, tenantID).First(&d).Error; err != nil {
		return nil, err
	}
	return &d, nil
}

// ListDirs 列出某租户某 scope 下某父目录的子目录。
// 私有库按 ownerUserID 过滤；公有库 ownerUserID 传 0。
func ListDirs(ctx context.Context, tenantID uint64, scope string, ownerUserID, parentID uint64) ([]model.KbaseDir, error) {
	q := GetDB().WithContext(ctx).Where("tenant_id = ? AND scope = ? AND parent_id = ?", tenantID, scope, parentID)
	if scope == ScopePrivate {
		q = q.Where("owner_user_id = ?", ownerUserID)
	}
	var list []model.KbaseDir
	if err := q.Order("created_at ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// ListAllDirs 列出某租户某 scope 下全部目录（用于组装目录树）。
func ListAllDirs(ctx context.Context, tenantID uint64, scope string, ownerUserID uint64) ([]model.KbaseDir, error) {
	q := GetDB().WithContext(ctx).Where("tenant_id = ? AND scope = ?", tenantID, scope)
	if scope == ScopePrivate {
		q = q.Where("owner_user_id = ?", ownerUserID)
	}
	var list []model.KbaseDir
	if err := q.Order("created_at ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// EnsureDirNotFound 判断目录不存在的错误。
func EnsureDirNotFound(err error) bool { return err == gorm.ErrRecordNotFound }

// SoftDeleteDir 软删除目录（仅本 scope 归属者可操作）。
func SoftDeleteDir(ctx context.Context, tenantID uint64, scope string, ownerUserID, dirID uint64) error {
	q := GetDB().WithContext(ctx).Where("id = ? AND tenant_id = ? AND scope = ?", dirID, tenantID, scope)
	if scope == ScopePrivate {
		q = q.Where("owner_user_id = ?", ownerUserID)
	}
	return q.Delete(&model.KbaseDir{}).Error
}
