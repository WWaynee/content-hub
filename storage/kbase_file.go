package storage

import (
	"context"

	"gorm.io/gorm"

	"github.com/WWaynee/content-hub/storage/model"
)

// 知识库文件 + 版本存储层。版本只增不减，latest 指针唯一。

// 文件状态常量
const (
	FileStatusPending    = "pending"
	FileStatusProcessing = "processing"
	FileStatusSuccess    = "success"
	FileStatusFail       = "fail"
)

// CreateFile 创建文件元数据（不含版本）。
func CreateFile(ctx context.Context, f *model.KbaseFile) error {
	return GetDB().WithContext(ctx).Create(f).Error
}

// GetFileByID 按 ID + 租户查文件（含软删过滤）。
func GetFileByID(ctx context.Context, tenantID, fileID uint64) (*model.KbaseFile, error) {
	var f model.KbaseFile
	if err := GetDB().WithContext(ctx).Where("id = ? AND tenant_id = ?", fileID, tenantID).First(&f).Error; err != nil {
		return nil, err
	}
	return &f, nil
}

// ListFilesByDir 列出某 scope 某目录下的文件（仅 active=1）。
// 私有库按 ownerUserID 过滤；公有库 ownerUserID 传 0。
func ListFilesByDir(ctx context.Context, tenantID uint64, scope string, ownerUserID, dirID uint64) ([]model.KbaseFile, error) {
	q := GetDB().WithContext(ctx).Where("tenant_id = ? AND scope = ? AND dir_id = ? AND active = 1", tenantID, scope, dirID)
	if scope == ScopePrivate {
		q = q.Where("owner_user_id = ?", ownerUserID)
	}
	var list []model.KbaseFile
	if err := q.Order("updated_at DESC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// ListAllFiles 列出某 scope 下的全部文件（仅 active=1，供"引用范围勾选"一次性取全量）。
// 私有库按 ownerUserID 过滤；公有库 ownerUserID 传 0。
func ListAllFiles(ctx context.Context, tenantID uint64, scope string, ownerUserID uint64) ([]model.KbaseFile, error) {
	q := GetDB().WithContext(ctx).Where("tenant_id = ? AND scope = ? AND active = 1", tenantID, scope)
	if scope == ScopePrivate {
		q = q.Where("owner_user_id = ?", ownerUserID)
	}
	var list []model.KbaseFile
	if err := q.Order("created_at ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// SearchFilesByName 按文件名模糊搜索（限租户 + scope + active）。
func SearchFilesByName(ctx context.Context, tenantID uint64, scope string, ownerUserID uint64, keyword string) ([]model.KbaseFile, error) {
	q := GetDB().WithContext(ctx).Where("tenant_id = ? AND scope = ? AND active = 1", tenantID, scope)
	if scope == ScopePrivate {
		q = q.Where("owner_user_id = ?", ownerUserID)
	}
	if keyword != "" {
		q = q.Where("name LIKE ?", "%"+keyword+"%")
	}
	var list []model.KbaseFile
	if err := q.Order("updated_at DESC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// ListFilesByIDs 批量按 ID 查文档元数据（限租户；不按 active 过滤）。
// 用途：证据 source 装配需要"绑定指向的文件即使已被删除(active=0)也能还原文件名，
// 并据 active/current_version_md5 计算 file_deleted 与 has_newer"——见 api/service/sources.go。
func ListFilesByIDs(ctx context.Context, tenantID uint64, ids []uint64) ([]model.KbaseFile, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var list []model.KbaseFile
	if err := GetDB().WithContext(ctx).Where("tenant_id = ? AND id IN ?", tenantID, ids).Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// CreateVersion 创建文档版本记录。用于上传新文件（versionNo=1）或覆盖（versionNo 递增）。
func CreateVersion(ctx context.Context, v *model.DocVersion) error {
	return GetDB().WithContext(ctx).Create(v).Error
}

// GetLatestVersion 取某文件当前 latest=1 的版本。
func GetLatestVersion(ctx context.Context, fileID uint64) (*model.DocVersion, error) {
	var v model.DocVersion
	if err := GetDB().WithContext(ctx).Where("file_id = ? AND latest = 1", fileID).First(&v).Error; err != nil {
		return nil, err
	}
	return &v, nil
}

// GetVersionByMd5 按 file_id + md5 查版本。
func GetVersionByMd5(ctx context.Context, fileID uint64, md5 string) (*model.DocVersion, error) {
	var v model.DocVersion
	if err := GetDB().WithContext(ctx).Where("file_id = ? AND version_md5 = ?", fileID, md5).First(&v).Error; err != nil {
		return nil, err
	}
	return &v, nil
}

// GetVersionByID 按版本 ID 查版本。
func GetVersionByID(ctx context.Context, versionID uint64) (*model.DocVersion, error) {
	var v model.DocVersion
	if err := GetDB().WithContext(ctx).Where("id = ?", versionID).First(&v).Error; err != nil {
		return nil, err
	}
	return &v, nil
}

// MarkVersionSuccess 标记某版本为成功（全链路完成），并把该文件 latest 指针切到此版本、旧版 latest 置 0。
// 用事务保证唯一 latest。
func MarkVersionSuccess(ctx context.Context, tenantID, fileID, versionID uint64, versionMd5 string) error {
	return GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. 该文件所有旧版本 latest=0
		if err := tx.Model(&model.DocVersion{}).Where("file_id = ?", fileID).Update("latest", 0).Error; err != nil {
			return err
		}
		// 2. 把目标版本置 latest=1，status=success
		if err := tx.Model(&model.DocVersion{}).Where("id = ?", versionID).Updates(map[string]interface{}{
			"latest": 1, "status": FileStatusSuccess, "error_msg": ""}).Error; err != nil {
			return err
		}
		// 3. 更新文件 current_version_md5 与 size
		return tx.Model(&model.KbaseFile{}).Where("id = ?", fileID).
			Update("current_version_md5", versionMd5).Error
	})
}

// MarkVersionFail 标记某版本为失败并记录原因（latest 不变，仍指向上一个成功版本）。
func MarkVersionFail(ctx context.Context, versionID uint64, errMsg string) error {
	return GetDB().WithContext(ctx).Model(&model.DocVersion{}).
		Where("id = ?", versionID).
		Updates(map[string]interface{}{"status": FileStatusFail, "error_msg": errMsg}).Error
}

// UpdateVersionStatus 更新版本状态（pending/processing 等）。
func UpdateVersionStatus(ctx context.Context, versionID uint64, status string) error {
	return GetDB().WithContext(ctx).Model(&model.DocVersion{}).Where("id = ?", versionID).Update("status", status).Error
}

// SoftDeleteFile 软删除文件（active=0，不再可见可检索，物理数据保留）。
func SoftDeleteFile(ctx context.Context, tenantID uint64, scope string, ownerUserID, fileID uint64) error {
	q := GetDB().WithContext(ctx).Model(&model.KbaseFile{}).
		Where("id = ? AND tenant_id = ? AND scope = ?", fileID, tenantID, scope)
	if scope == ScopePrivate {
		q = q.Where("owner_user_id = ?", ownerUserID)
	}
	return q.Update("active", 0).Error
}

// RenameFile 重命名文件（仅本 scope 归属者可操作）。返回受影响行数，越权时为 0。
func RenameFile(ctx context.Context, tenantID uint64, scope string, ownerUserID, fileID uint64, name string) (int64, error) {
	q := GetDB().WithContext(ctx).Model(&model.KbaseFile{}).
		Where("id = ? AND tenant_id = ? AND scope = ?", fileID, tenantID, scope)
	if scope == ScopePrivate {
		q = q.Where("owner_user_id = ?", ownerUserID)
	}
	res := q.Update("name", name)
	return res.RowsAffected, res.Error
}
