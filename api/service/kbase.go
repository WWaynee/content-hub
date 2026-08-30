package service

import (
	"context"
	"fmt"
	"unicode/utf8"

	"github.com/WWaynee/content-hub/config"
	"github.com/WWaynee/content-hub/llmclient"
	"github.com/WWaynee/content-hub/observability"
	"github.com/WWaynee/content-hub/splitter"
	"github.com/WWaynee/content-hub/storage"
	"github.com/WWaynee/content-hub/storage/model"
)

// 知识库解析向量化主流程。

var ErrFileNotFound = fmt.Errorf("文件不存在或无权访问")

// ProcessDocument 把一个已上传的文档版本做「切片 → 句子 → embedding → 写入 Qdrant」全链路，
// 全链路成功后才该版本 latest=1 成为可检索最新版。
//
// 失败时：版本状态置 fail，latest 不变（仍指向上一个成功版本）。
func ProcessDocument(ctx context.Context, tenantID, fileID, versionID uint64) error {
	ver, err := storage.GetVersionByID(ctx, versionID)
	if err != nil {
		return ErrFileNotFound
	}
	if ver.FileID != fileID || ver.TenantID != tenantID {
		return ErrFileNotFound
	}

	// 1. 标记 processing
	if err := storage.UpdateVersionStatus(ctx, versionID, storage.FileStatusProcessing); err != nil {
		return fmt.Errorf("更新版本为处理中失败: %w", err)
	}

	// 2. 下载并读取文本
	data, err := storage.DownloadFile(ver.OSSObjectKey)
	if err != nil {
		return failVersion(ctx, versionID, fmt.Errorf("下载文档失败: %w", err))
	}
	text := string(data)

	// 3. 切片（完整句末软截断）
	size := config.Get().Chunk.Size
	chunks := splitter.Split(text, size)
	if len(chunks) == 0 {
		return failVersion(ctx, versionID, fmt.Errorf("文档内容为空，无可切片文本"))
	}

	// 4. 切片落 MySQL + 句子切分落 MySQL
	var chunkRecords []model.DocChunk
	sentenceMap := make(map[int][]model.DocSentence) // chunk index -> sentences
	for i, c := range chunks {
		chunkRecords = append(chunkRecords, model.DocChunk{
			TenantID:     tenantID,
			FileID:       fileID,
			VersionMd5:   ver.VersionMd5,
			ChunkIndex:   i,
			ChapterTitle: c.ChapterTitle,
			Content:      c.Content,
		})
		// 句子切分，累计切片内偏移
		sents := splitter.Sentences(c.Content)
		offset := 0
		for j, s := range sents {
			sentenceMap[i] = append(sentenceMap[i], model.DocSentence{
				TenantID:      tenantID,
				FileID:        fileID,
				VersionMd5:    ver.VersionMd5,
				SentenceIndex: j,
				Content:       s,
				StartChar:     offset,
				EndChar:       offset + utf8.RuneCountInString(s),
			})
			offset += utf8.RuneCountInString(s)
		}
	}

	// 先写切片（拿 chunk_id），再写句子
	if err := storage.BatchCreateChunks(ctx, chunkRecords); err != nil {
		return failVersion(ctx, versionID, fmt.Errorf("切片落库失败: %w", err))
	}
	for i, sents := range sentenceMap {
		for j := range sents {
			sents[j].ChunkID = chunkRecords[i].ID
		}
		if err := storage.GetDB().WithContext(ctx).Create(&sents).Error; err != nil {
			return failVersion(ctx, versionID, fmt.Errorf("句子落库失败: %w", err))
		}
	}

	// 5. embedding 批量
	llm := llmclient.NewClient()
	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = c.Content
	}
	vectors, err := llm.EmbedBatch(ctx, texts)
	if err != nil {
		return failVersion(ctx, versionID, fmt.Errorf("向量化失败: %w", err))
	}
	if len(vectors) != len(chunks) {
		return failVersion(ctx, versionID, fmt.Errorf("向量数量不符: 期望 %d 实际 %d", len(chunks), len(vectors)))
	}

	// 6. 写入 Qdrant
	points := make([]storage.QdrantVector, 0, len(chunks))
	for i, c := range chunks {
		points = append(points, storage.QdrantVector{
			ID:           ComposePointID(fileID, ver.VersionMd5, i),
			TenantID:     tenantID,
			FileID:       fileID,
			VersionMd5:   ver.VersionMd5,
			ChunkIndex:   i,
			Content:      c.Content,
			ChapterTitle: c.ChapterTitle,
			Latest:       true,
			Vector:       vectors[i],
		})
	}
	if err := storage.UpsertVectors(ctx, points); err != nil {
		return failVersion(ctx, versionID, fmt.Errorf("写入向量库失败: %w", err))
	}

	// 7. 标记版本成功（自动把旧版 latest 置 0、本版本 latest=1）
	if err := storage.MarkVersionSuccess(ctx, tenantID, fileID, versionID, ver.VersionMd5); err != nil {
		return fmt.Errorf("标记版本成功失败: %w", err)
	}

	observability.WithContext(ctx).Info("文档向量化完成",
		map[string]interface{}{"file_id": fileID, "version": ver.VersionMd5, "chunks": len(chunks)})
	return nil
}

func failVersion(ctx context.Context, versionID uint64, cause error) error {
	if err := storage.MarkVersionFail(ctx, versionID, cause.Error()); err != nil {
		return fmt.Errorf("%v；标记失败状态报错: %w", cause, err)
	}
	return cause
}

// ComposePointID 合成向量点全局唯一 ID。
// content-hub 有版本概念，同一 file 不同版本 chunkIndex 会相同，故必须把 version 纳入 ID，
// 否则不同版本会互相覆盖向量点。用 fileID 高 32 位 + version hash + chunkIndex 混合保证唯一。
func ComposePointID(fileID uint64, versionMd5 string, chunkIndex int) uint64 {
	h := fnvHash(versionMd5)
	return (fileID << 32) ^ (uint64(h) << 12) | uint64(chunkIndex)
}

func fnvHash(s string) uint32 {
	h := uint32(2166136261)
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return h
}
