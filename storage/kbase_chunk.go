package storage

import (
	"context"

	"github.com/WWaynee/content-hub/storage/model"
)

// 切片/句存储层。切片与句都携带版本，旧版本不可检索但原文保留。

// CreateChunk 写入切片原文（幂等由 (file_id,version_md5,chunk_index) 唯一索引保证）。
func CreateChunk(ctx context.Context, c *model.DocChunk) error {
	return GetDB().WithContext(ctx).Create(c).Error
}

// BatchCreateChunks 批量写切片。
func BatchCreateChunks(ctx context.Context, chunks []model.DocChunk) error {
	if len(chunks) == 0 {
		return nil
	}
	return GetDB().WithContext(ctx).Create(&chunks).Error
}

// CreateSentence 写入文档句（去重由业务层保证，这里仅落库）。
func CreateSentence(ctx context.Context, s *model.DocSentence) error {
	return GetDB().WithContext(ctx).Create(s).Error
}

// ListChunksByVersion 按 file+version 列出全部切片（按 chunk_index 升序）。
func ListChunksByVersion(ctx context.Context, tenantID, fileID uint64, versionMd5 string) ([]model.DocChunk, error) {
	var list []model.DocChunk
	if err := GetDB().WithContext(ctx).
		Where("tenant_id = ? AND file_id = ? AND version_md5 = ?", tenantID, fileID, versionMd5).
		Order("chunk_index ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// GetChunkByVersionIndex 按 file+version+chunk_index 查单个切片。
func GetChunkByVersionIndex(ctx context.Context, tenantID, fileID uint64, versionMd5 string, chunkIndex int) (*model.DocChunk, error) {
	var c model.DocChunk
	if err := GetDB().WithContext(ctx).
		Where("tenant_id = ? AND file_id = ? AND version_md5 = ? AND chunk_index = ?",
			tenantID, fileID, versionMd5, chunkIndex).
		First(&c).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

// ListSentencesByChunk 列出某切片下的全部句子。
func ListSentencesByChunk(ctx context.Context, chunkID uint64) ([]model.DocSentence, error) {
	var list []model.DocSentence
	if err := GetDB().WithContext(ctx).Where("chunk_id = ?", chunkID).
		Order("sentence_index ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// GetSentenceByID 按 ID 查句子（用于证据绑定回读原文）。
func GetSentenceByID(ctx context.Context, sentenceID uint64) (*model.DocSentence, error) {
	var s model.DocSentence
	if err := GetDB().WithContext(ctx).Where("id = ?", sentenceID).First(&s).Error; err != nil {
		return nil, err
	}
	return &s, nil
}

// DeleteChunksAndSentencesByVersion 删除某 file+version 的切片与句子（幂等清理，防失败重跑撞唯一索引）。
func DeleteChunksAndSentencesByVersion(ctx context.Context, tenantID, fileID uint64, versionMd5 string) error {
	if err := GetDB().WithContext(ctx).
		Where("tenant_id = ? AND file_id = ? AND version_md5 = ?", tenantID, fileID, versionMd5).
		Delete(&model.DocChunk{}).Error; err != nil {
		return err
	}
	if err := GetDB().WithContext(ctx).
		Where("tenant_id = ? AND file_id = ? AND version_md5 = ?", tenantID, fileID, versionMd5).
		Delete(&model.DocSentence{}).Error; err != nil {
		return err
	}
	return nil
}
