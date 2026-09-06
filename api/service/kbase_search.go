package service

import (
	"context"
	"fmt"

	"github.com/WWaynee/content-hub/config"
	"github.com/WWaynee/content-hub/llmclient"
	"github.com/WWaynee/content-hub/observability"
	"github.com/WWaynee/content-hub/storage"
)

// searchOwnerFromCtx 返回 ctx 中登记的检索者 user_id（可见性 owner）：
//   - user_id>0：本次检索可见范围 = 公库 OR 该用户私有库；
//   - user_id==0（无登记 ctx，如后台/worker/未代 user 的调用）：仅公库（保守，不越权看私库）。
//
// 由 middleware 通过 observability.WithTenantUser 注入；HTTP 请求必带 user。
func searchOwnerFromCtx(ctx context.Context) uint64 {
	return observability.UserIDFromCtx(ctx)
}

// Evidence 一次检索返回的证据片段。
type Evidence struct {
	FileID       uint64  // 来源文档
	VersionMd5   string  // 版本
	ChunkIndex   int     // 切片序号
	ChapterTitle string  // 章节标题（可空）
	SourceText   string  // 原始原文片段（不加工）
	Score        float32 // 相似度
}

// SearchKbase 在租户的知识库内检索，返回 top-K 证据片段（切片粒度，供撰写 agent 使用）。
// 强制 tenant + latest 过滤，并按 ctx 中检索者身份限定可见范围（见 kbaseSearchOwner）；
// fileIDs 非空则限定到具体文档。
func SearchKbase(ctx context.Context, tenantID uint64, query string, fileIDs ...uint64) ([]Evidence, error) {
	llm := llmclient.NewClient()
	vec, err := llm.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("查询向量化失败: %w", err)
	}
	topK := config.Get().Retrieval.TopK
	minScore := config.Get().Retrieval.MinScore
	hits, err := storage.SearchVectors(ctx, vec, tenantID, searchOwnerFromCtx(ctx), topK, fileIDs...)
	if err != nil {
		return nil, err
	}
	out := make([]Evidence, 0, len(hits))
	for _, h := range hits {
		// 相似度阈值过滤：低于阈值的命中判为「不相关」，不作为证据（避免低相关噪声进入撰写/问答）
		if minScore > 0 && h.Score < minScore {
			continue
		}
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

// SearchKbaseSentences 句子级检索已按 P14 落为可配置策略（见 kbase_strategy.go）：
// SearchSentencesByStrategy(ctx, tenantID, query, DefaultSentenceSearchOptions(), fileIDs...)
// 默认 Strategy=dense，行为与 P14 之前完全一致。
