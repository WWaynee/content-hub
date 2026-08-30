package service

import (
	"context"
	"fmt"

	"github.com/WWaynee/content-hub/config"
	"github.com/WWaynee/content-hub/llmclient"
	"github.com/WWaynee/content-hub/storage"
)

// Evidence 一次检索返回的证据片段。
type Evidence struct {
	FileID       uint64  // 来源文档
	VersionMd5   string  // 版本
	ChunkIndex   int     // 切片序号
	ChapterTitle string  // 章节标题（可空）
	SourceText   string  // 原始原文片段（不加工）
	Score        float32 // 相似度
}

// SearchKbase 在租户的知识库内检索，返回 top-K 证据片段。
// 强制 tenant + latest 过滤；fileIDs 为空限定到具体文档（勾选范围），不传则全租户。
func SearchKbase(ctx context.Context, tenantID uint64, query string, fileIDs ...uint64) ([]Evidence, error) {
	llm := llmclient.NewClient()
	vec, err := llm.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("查询向量化失败: %w", err)
	}

	topK := config.Get().Retrieval.TopK
	hits, err := storage.SearchVectors(ctx, vec, tenantID, topK, fileIDs...)
	if err != nil {
		return nil, err
	}

	out := make([]Evidence, 0, len(hits))
	for _, h := range hits {
		out = append(out, Evidence{
			FileID:       h.FileID,
			VersionMd5:   h.VersionMd5,
			ChunkIndex:   h.ChunkIndex,
			ChapterTitle: h.ChapterTitle,
			SourceText:   h.Content,
			Score:        h.Score,
		})
	}
	return out, nil
}
