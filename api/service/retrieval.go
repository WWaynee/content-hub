package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/WWaynee/content-hub/agent"
	"github.com/WWaynee/content-hub/storage"
	"github.com/WWaynee/content-hub/storage/model"

	"gorm.io/gorm"
)

// 检索快照落库 + 惰性失效判定。

// PersistRetrievalBatch 把一次检索结果落为检索快照（batch + items 指针）。
func PersistRetrievalBatch(ctx context.Context, tenantID, workspaceID, requirementID uint64, requirementVersion int, queries []string, hits []KbaseHit) (uint64, error) {
	qJSON, _ := json.Marshal(queries)
	batch := &model.RetrievalBatch{
		TenantID:           tenantID,
		WorkspaceID:        workspaceID,
		RequirementID:      requirementID,
		RequirementVersion: requirementVersion,
		Queries:            string(qJSON),
	}
	items := make([]model.RetrievalBatchItem, 0, len(hits))
	for _, h := range hits {
		items = append(items, model.RetrievalBatchItem{
			DocSentenceID: h.DocSentenceID,
			DocFileID:     h.FileID,
			VersionMd5:    h.VersionMd5,
			ChunkID:       h.ChunkID,
			ChapterTitle:  h.ChapterTitle,
			Score:         float64(h.Score),
		})
	}
	if err := storage.CreateRetrievalBatch(ctx, batch, items); err != nil {
		return 0, fmt.Errorf("落检索快照失败: %w", err)
	}
	return batch.ID, nil
}

// IsBatchStale 判定检索快照是否过期：batch 的 requirement_version != 当前需求单 version。
func IsBatchStale(ctx context.Context, batchID, requirementID uint64) (bool, error) {
	batch, err := storage.GetRetrievalBatch(ctx, batchID)
	if err != nil {
		return false, fmt.Errorf("查检索快照失败: %w", err)
	}
	req, err := storage.GetRequirementByID(ctx, requirementID)
	if err != nil {
		return false, fmt.Errorf("查需求单失败: %w", err)
	}
	return batch.RequirementVersion != req.Version, nil
}

// EnsureBatchFresh 惰性失效判定：局部修订/追加前检查最新检索快照是否仍与需求单版本一致。
//   - 最新 batch 不存在（尚未生成过）→ 放行（无旧证据可失效）。
//   - 最新 batch 存在且 version 一致 → 放行。
//   - 最新 batch 存在但 version 不一致（需求单改过）→ 拒绝，要求先重新生成。
func EnsureBatchFresh(ctx context.Context, workspaceID, requirementID uint64) error {
	batch, err := storage.GetLatestRetrievalBatch(ctx, workspaceID)
	if err == gorm.ErrRecordNotFound {
		return nil // 尚未检索过，无失效可言
	}
	if err != nil {
		return fmt.Errorf("查最新检索快照失败: %w", err)
	}
	stale, err := IsBatchStale(ctx, batch.ID, requirementID)
	if err != nil {
		return err
	}
	if stale {
		return fmt.Errorf("需求单已变更，当前检索快照已过期，请先重新生成稿件")
	}
	return nil
}

// EvidenceToKbaseHits 把 generation 产出的 agent.Evidence 转成 KbaseHit（供落检索快照）。
func EvidenceToKbaseHits(evs []agent.Evidence) []KbaseHit {
	out := make([]KbaseHit, 0, len(evs))
	for _, e := range evs {
		out = append(out, KbaseHit{
			FileID:        e.FileID,
			DocSentenceID: e.DocSentenceID,
			VersionMd5:    e.VersionMd5,
			ChunkID:       e.ChunkID,
			ChapterTitle:  e.ChapterTitle,
			SourceText:    e.SourceText,
			Score:         e.Score,
		})
	}
	return out
}

// DiffBatchSentenceIDs 计算旧 batch 与新 batch 的 doc_sentence_id 差异（新增/删除）。
func DiffBatchSentenceIDs(ctx context.Context, oldBatchID, newBatchID uint64) (added, removed []uint64, err error) {
	oldIDs, err := storage.ListBatchSentenceIDs(ctx, oldBatchID)
	if err != nil {
		return nil, nil, err
	}
	newIDs, err := storage.ListBatchSentenceIDs(ctx, newBatchID)
	if err != nil {
		return nil, nil, err
	}
	oldSet := map[uint64]bool{}
	for _, id := range oldIDs {
		oldSet[id] = true
	}
	newSet := map[uint64]bool{}
	for _, id := range newIDs {
		newSet[id] = true
	}
	for _, id := range newIDs {
		if !oldSet[id] {
			added = append(added, id)
		}
	}
	for _, id := range oldIDs {
		if !newSet[id] {
			removed = append(removed, id)
		}
	}
	return added, removed, nil
}

// KbaseHit 检索命中的证据指针（含 doc_sentence_id），供落快照复用。
// 与 agent.Evidence / service.Evidence 对齐，补 doc_sentence_id + chunk_id。
type KbaseHit struct {
	FileID       uint64
	DocSentenceID uint64
	VersionMd5   string
	ChunkID      uint64
	ChapterTitle string
	SourceText   string
	Score        float32
}
