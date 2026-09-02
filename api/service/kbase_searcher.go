package service

import (
	"context"

	"github.com/WWaynee/content-hub/agent"
	"github.com/WWaynee/content-hub/agent/censor"
)

// KbaseSearcher 实现 censor.Searcher，供 ClaimPlanner 逐点检索。
// 复用 SearchKbaseSentences（含相似度阈值过滤），并把句子级命中转成 agent.Evidence。
type KbaseSearcher struct{}

// NewKbaseSearcher 构造。
func NewKbaseSearcher() *KbaseSearcher { return &KbaseSearcher{} }

// Compile-time 断言：KbaseSearcher 实现 censor.Searcher。
var _ censor.Searcher = (*KbaseSearcher)(nil)

func (k *KbaseSearcher) SearchSentences(ctx context.Context, tenantID uint64, query string, fileIDs ...uint64) ([]agent.Evidence, error) {
	hits, err := SearchKbaseSentences(ctx, tenantID, query, fileIDs...)
	if err != nil {
		return nil, err
	}
	out := make([]agent.Evidence, 0, len(hits))
	for _, h := range hits {
		out = append(out, agent.Evidence{
			FileID:        h.FileID,
			DocSentenceID: h.DocSentenceID,
			ChunkID:       h.ChunkID,
			VersionMd5:    h.VersionMd5,
			ChapterTitle:  h.ChapterTitle,
			SourceText:    h.SourceText,
			Score:         h.Score,
		})
	}
	return out, nil
}
