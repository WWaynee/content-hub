package service

import (
	"context"
	"fmt"
	"math"
	"sort"

	"github.com/WWaynee/content-hub/config"
	"github.com/WWaynee/content-hub/llmclient"
	"github.com/WWaynee/content-hub/splitter"
	"github.com/WWaynee/content-hub/storage"
)

// P14 检索质量升级：把「句子级检索」从单一纯向量实现，开放为可配置的排序策略链。
//
// 依赖方向说明（相对 RFC/任务书的落点修正）：句子展开（chunk → doc_sentence）本就在
// service 层（SearchKbaseSentences），故策略层实现于此，而不是 storage/qdrant.go
// （storage 只有向量检索）或 agent/retrieve（主链路实际经 censor.ClaimPlanner 调用本包）。
//
// 策略链（均为可选叠加，默认 dense = 纯向量现状）：
//   dense            纯向量（现状，默认）
//   hybrid           dense 候选池 top HybridK → 池内 BM25(2-gram) + RRF 融合
//   hybrid_rerank    hybrid 之后再取前 RerankTopN 用跨编码器 rerank 精排

// SearchStrategy 检索排序策略名（与 config.Retrieval.Strategy / RETRIEVAL_STRATEGY 对齐）。
type SearchStrategy string

const (
	// StrategyDense 纯向量检索（默认，即 P14 之前的现状行为）。
	StrategyDense SearchStrategy = "dense"
	// StrategyHybrid 混合检索：BM25 关键词 + Dense 向量，RRF 融合。
	StrategyHybrid SearchStrategy = "hybrid"
	// StrategyHybridRerank 混合检索 + 跨编码器 Rerank 精排。
	StrategyHybridRerank SearchStrategy = "hybrid_rerank"
)

// SentenceSearchOptions 一次句子检索的显式参数（生产走 config 默认；评测/测试可覆盖）。
type SentenceSearchOptions struct {
	TopK     int
	MinScore float32
	Strategy SearchStrategy

	// hybrid
	HybridK int
	RRFK    int
	BM25K1  float64
	BM25B   float64

	// rerank（hybrid_rerank）
	RerankTopN    int
	RerankModel   string
	RerankBaseURL string
	RerankAPIKey  string
}

// DefaultSentenceSearchOptions 从全局配置组装检索参数（config.Load 之后调用）。
func DefaultSentenceSearchOptions() SentenceSearchOptions {
	r := config.Get().Retrieval
	return SentenceSearchOptions{
		TopK:     r.TopK,
		MinScore: r.MinScore,
		Strategy: SearchStrategy(r.Strategy),

		HybridK: r.HybridK,
		RRFK:    r.RRFK,
		BM25K1:  r.BM25K1,
		BM25B:   r.BM25B,

		RerankTopN:    r.RerankTopN,
		RerankModel:   r.RerankModel,
		RerankBaseURL: r.RerankBaseURL,
		RerankAPIKey:  r.RerankAPIKey,
	}
}

// SearchKbaseSentences 句子级证据检索（主链路默认入口，行为与 P14 之前完全一致：
// 默认 Strategy=dense 时即纯向量路径；通过 config 开启 hybrid/hybrid_rerank 后无缝升级）。
func SearchKbaseSentences(ctx context.Context, tenantID uint64, query string, fileIDs ...uint64) ([]KbaseHit, error) {
	return SearchSentencesByStrategy(ctx, tenantID, query, DefaultSentenceSearchOptions(), fileIDs...)
}

// SearchSentencesByStrategy 按显式策略执行句子级检索（评测基准与测试统一入口）。
func SearchSentencesByStrategy(ctx context.Context, tenantID uint64, query string, opt SentenceSearchOptions, fileIDs ...uint64) ([]KbaseHit, error) {
	if opt.TopK <= 0 {
		opt.TopK = 20
	}
	switch opt.Strategy {
	case StrategyHybrid:
		return hybridSearch(ctx, tenantID, query, opt, fileIDs...)
	case StrategyHybridRerank:
		return hybridRerankSearch(ctx, tenantID, query, opt, fileIDs...)
	default: // dense 与未知值一律走纯向量（未知策略名按现状保守处理）
		return denseSearch(ctx, tenantID, query, opt, fileIDs...)
	}
}

// denseSearch 纯向量句子检索（= P14 之前 SearchKbaseSentences 的原始实现，语义逐条等价）。
func denseSearch(ctx context.Context, tenantID uint64, query string, opt SentenceSearchOptions, fileIDs ...uint64) ([]KbaseHit, error) {
	vec, err := embedQuery(ctx, query)
	if err != nil {
		return nil, err
	}
	hits, err := storage.SearchVectors(ctx, vec, tenantID, searchOwnerFromCtx(ctx), opt.TopK, fileIDs...)
	if err != nil {
		return nil, err
	}
	return expandChunkHits(ctx, tenantID, hits, opt.MinScore), nil
}

// embedQuery 查询向量化（策略公共）。
func embedQuery(ctx context.Context, query string) ([]float32, error) {
	vec, err := llmclient.NewClient().Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("查询向量化失败: %w", err)
	}
	return vec, nil
}

// expandChunkHits 把命中的切片按给定顺序展开为句子级 KbaseHit（沿用现状语义：
// 阈值过滤 → 反查切片 → 该切片内全部句子按 sentence_index 顺序展开 → doc_sentence_id 去重）。
// minScore<=0 表示不做相似度阈值过滤（hybrid/rerank 的排序由融合/重排分数决定，语义不同于向量分）。
func expandChunkHits(ctx context.Context, tenantID uint64, hits []storage.QdrantSearchHit, minScore float32) []KbaseHit {
	var out []KbaseHit
	seen := map[uint64]bool{}
	for _, h := range hits {
		if minScore > 0 && h.Score < minScore {
			continue
		}
		chunk, err := storage.GetChunkByVersionIndex(ctx, tenantID, h.FileID, h.VersionMd5, h.ChunkIndex)
		if err != nil {
			continue // 切片缺失则跳过（防御）
		}
		sents, err := storage.ListSentencesByChunk(ctx, chunk.ID)
		if err != nil {
			continue
		}
		for _, s := range sents {
			if seen[s.ID] {
				continue
			}
			seen[s.ID] = true
			out = append(out, KbaseHit{
				FileID:        h.FileID,
				DocSentenceID: s.ID,
				VersionMd5:    h.VersionMd5,
				ChunkID:       chunk.ID,
				ChapterTitle:  h.ChapterTitle,
				SourceText:    s.Content,
				Score:         h.Score,
			})
		}
	}
	return out
}

// ---- hybrid：BM25(2-gram) + Dense，RRF 融合 ----

// bm25Candidate 候选池中一个切片的两路排名输入。
type bm25Candidate struct {
	hit       storage.QdrantSearchHit
	bm25Score float64
}

// hybridSearch 混合检索：向量召回 top HybridK 作候选池 → 池内对 query 与每 chunk 做
// BM25(2-gram) → RRF 融合 dense 名次与 BM25 名次 → 取前 TopK 展开句子。
// 说明：不按向量 minScore 过滤——混合的意义正是把「向量相似度低但关键词强匹配」的
// 专名/编号片段捞回来，最终排序由 RRF 决定。
func hybridSearch(ctx context.Context, tenantID uint64, query string, opt SentenceSearchOptions, fileIDs ...uint64) ([]KbaseHit, error) {
	hybridK := opt.HybridK
	if hybridK <= opt.TopK {
		hybridK = opt.TopK * 2 // 候选池至少要大于最终返回数
	}
	vec, err := embedQuery(ctx, query)
	if err != nil {
		return nil, err
	}
	pool, err := storage.SearchVectors(ctx, vec, tenantID, searchOwnerFromCtx(ctx), hybridK, fileIDs...)
	if err != nil {
		return nil, err
	}
	if len(pool) == 0 {
		return nil, nil
	}
	ranked := rrfFuse(query, pool, opt)
	if len(ranked) > opt.TopK {
		ranked = ranked[:opt.TopK]
	}
	return expandChunkHits(ctx, tenantID, ranked, 0), nil
}

// rrfFuse 计算 BM25 分数并按 RRF 融合排序，返回按融合分降序的切片列表。
func rrfFuse(query string, pool []storage.QdrantSearchHit, opt SentenceSearchOptions) []storage.QdrantSearchHit {
	k1, b := opt.BM25K1, opt.BM25B
	if k1 <= 0 {
		k1 = 1.2
	}
	if b <= 0 {
		b = 0.75
	}
	rrfK := opt.RRFK
	if rrfK <= 0 {
		rrfK = 60
	}

	queryTerms := splitter.CharNGramTokens(query, 2)
	// 池内统计：每 doc 的 token 频次 + df + 文档长（用于 avgdl）
	type docTerms struct {
		tf   map[string]int
		total int
	}
	docs := make([]docTerms, len(pool))
	df := map[string]int{}
	var sumLen int
	for i, h := range pool {
		toks := splitter.CharNGramTokens(h.Content, 2)
		total := 0
		for _, n := range toks {
			total += n
		}
		docs[i] = docTerms{tf: toks, total: total}
		for term := range toks {
			if _, seen := df[term]; !seen {
				df[term] = 1
			} else {
				df[term]++
			}
		}
		sumLen += total
	}
	n := len(pool)
	avgdl := float64(sumLen) / float64(n)

	// BM25 打分
	scores := make([]float64, n)
	for i := range pool {
		d := docs[i]
		var s float64
		for term := range queryTerms {
			tf, ok := d.tf[term]
			if !ok {
				continue
			}
			idf := math.Log(1 + (float64(n)-float64(df[term])+0.5)/(float64(df[term])+0.5))
			denom := float64(tf) + k1*(1-b+b*float64(d.total)/avgdl)
			s += idf * (float64(tf) * (k1 + 1)) / denom
		}
		scores[i] = s
	}

	// RRF：对 dense 名次与 BM25 名次分别取倒数融合。dense 名次即向量检索返回序。
	bm25Order := make([]int, n)
	for i := range bm25Order {
		bm25Order[i] = i
	}
	sort.SliceStable(bm25Order, func(a, b int) bool { return scores[bm25Order[a]] > scores[bm25Order[b]] })
	bm25Rank := make(map[int]int, n)
	for r, idx := range bm25Order {
		bm25Rank[idx] = r
	}

	type fused struct {
		hit storage.QdrantSearchHit
		rrf float64
	}
	out := make([]fused, n)
	for i, h := range pool {
		out[i] = fused{
			hit: h,
			rrf: 1.0/(float64(rrfK)+float64(i)+1) + 1.0/(float64(rrfK)+float64(bm25Rank[i])+1),
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].rrf > out[b].rrf })
	ranked := make([]storage.QdrantSearchHit, n)
	for i := range out {
		ranked[i] = out[i].hit
	}
	return ranked
}

// ---- hybrid_rerank：hybrid 之后再用跨编码器 rerank 精排 top-N ----

// hybridRerankSearch 混合检索取前 RerankTopN 候选 → rerank 精排 → 取前 TopK 展开。
// rerank 不可用（未配置/调用失败）时降级为 hybrid 结果并只记日志，不硬抛打断主流程。
func hybridRerankSearch(ctx context.Context, tenantID uint64, query string, opt SentenceSearchOptions, fileIDs ...uint64) ([]KbaseHit, error) {
	hybridK := opt.HybridK
	if hybridK <= opt.TopK {
		hybridK = opt.TopK * 2
	}
	vec, err := embedQuery(ctx, query)
	if err != nil {
		return nil, err
	}
	pool, err := storage.SearchVectors(ctx, vec, tenantID, searchOwnerFromCtx(ctx), hybridK, fileIDs...)
	if err != nil {
		return nil, err
	}
	if len(pool) == 0 {
		return nil, nil
	}
	ranked := rrfFuse(query, pool, opt)

	rerankTopN := opt.RerankTopN
	if rerankTopN <= 0 {
		rerankTopN = 20
	}
	if rerankTopN > len(ranked) {
		rerankTopN = len(ranked)
	}
	cands := ranked[:rerankTopN]

	docs := make([]string, len(cands))
	for i, h := range cands {
		docs[i] = h.Content
	}
	res, rerr := llmclient.GetReranker().Rerank(ctx, query, docs, opt.TopK)
	if rerr != nil {
		// rerank 失败：降级为 hybrid（取前 TopK），保证检索不因精排层故障而整体失败
		if len(cands) > opt.TopK {
			cands = cands[:opt.TopK]
		}
		return expandChunkHits(ctx, tenantID, cands, 0), nil
	}

	// 按 rerank 相关度降序重排候选
	byIndex := make(map[int]storage.QdrantSearchHit, len(cands))
	for i, h := range cands {
		byIndex[i] = h
	}
	reranked := make([]storage.QdrantSearchHit, 0, len(res))
	for _, rr := range res {
		if h, ok := byIndex[rr.Index]; ok {
			reranked = append(reranked, h)
		}
	}
	// rerank 结果未覆盖全部候选时补齐（防御）
	for i, h := range cands {
		if len(reranked) >= opt.TopK {
			break
		}
		covered := false
		for _, rr := range res {
			if rr.Index == i {
				covered = true
				break
			}
		}
		if !covered {
			reranked = append(reranked, h)
		}
	}
	if len(reranked) > opt.TopK {
		reranked = reranked[:opt.TopK]
	}
	return expandChunkHits(ctx, tenantID, reranked, 0), nil
}
