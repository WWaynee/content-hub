package storage

import (
	"context"

	"gorm.io/gorm"

	"github.com/WWaynee/content-hub/storage/model"
)

// 检索快照存储层（方案甲：batch + items 关联表）。

// CreateRetrievalBatch 创建检索快照（含 items），事务保证 batch 与 items 一致。
func CreateRetrievalBatch(ctx context.Context, batch *model.RetrievalBatch, items []model.RetrievalBatchItem) error {
	return GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(batch).Error; err != nil {
			return err
		}
		if len(items) > 0 {
			for i := range items {
				items[i].BatchID = batch.ID
				items[i].TenantID = batch.TenantID
			}
			if err := tx.Create(&items).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// GetRetrievalBatch 按 ID 查检索快照。
func GetRetrievalBatch(ctx context.Context, batchID uint64) (*model.RetrievalBatch, error) {
	var b model.RetrievalBatch
	if err := GetDB().WithContext(ctx).Where("id = ?", batchID).First(&b).Error; err != nil {
		return nil, err
	}
	return &b, nil
}

// ListBatchItems 列出某批次的全部命中指针（按 doc_sentence_id 升序，便于 diff）。
func ListBatchItems(ctx context.Context, batchID uint64) ([]model.RetrievalBatchItem, error) {
	var items []model.RetrievalBatchItem
	if err := GetDB().WithContext(ctx).Where("batch_id = ?", batchID).
		Order("doc_sentence_id ASC").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// ListBatchSentenceIDs 返回某批次的 doc_sentence_id 有序列表（用于 diff）。
func ListBatchSentenceIDs(ctx context.Context, batchID uint64) ([]uint64, error) {
	items, err := ListBatchItems(ctx, batchID)
	if err != nil {
		return nil, err
	}
	ids := make([]uint64, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.DocSentenceID)
	}
	return ids, nil
}
