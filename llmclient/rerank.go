package llmclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/WWaynee/content-hub/config"
)

// Reranker 重排序能力：query × 候选文档的交叉编码精排（比向量相似度更细粒度）。
// P14 第二层：拿到向量/混合检索的 top-N 后，用 rerank 模型重新排序。
type Reranker interface {
	// Rerank 返回按相关度降序的 (原文下标 → 分数)，最多 topN 条。
	Rerank(ctx context.Context, query string, docs []string, topN int) ([]RerankScore, error)
}

// RerankScore 一条重排序结果。
type RerankScore struct {
	Index int     // 在传入 docs 中的下标
	Score float32 // relevance_score
}

// ErrRerankNotConfigured 未配置 rerank 服务（base_url/api_key 为空）。
var ErrRerankNotConfigured = errors.New("rerank 服务未配置（RETRIEVAL_RERANK_BASE_URL/API_KEY 或 Embedding 段为空）")

// ErrRerankNotReady 重排序器尚未初始化（防御性哨兵，与 storage.ErrQdrantNotReady 同思路）。
var ErrRerankNotReady = errors.New("rerank 客户端未初始化")

// reranker 全局实例（NewReranker 后可用）。
var reranker Reranker

// SiliconFlowReranker 硅基流动 rerank 实现（OpenAI 兼容 rerank 形态：POST /rerank）。
// 复用 OpenAIClient 的按负载超时预算 / 独立重试预算 / 三态熔断，保证与 chat/embedding 同等的工程兜底。
type SiliconFlowReranker struct {
	cli     *OpenAIClient
	baseURL string
	apiKey  string
	model   string
}

// NewSiliconFlowReranker 构造重排序器。base_url/api_key 优先取 Retrieval.Rerank* 配置，
// 为空时回落 Embedding 段（默认同为硅基流动）。
func NewSiliconFlowReranker() *SiliconFlowReranker {
	cfg := config.Get()
	baseURL := cfg.Retrieval.RerankBaseURL
	if baseURL == "" {
		baseURL = cfg.Embedding.BaseURL
	}
	apiKey := cfg.Retrieval.RerankAPIKey
	if apiKey == "" {
		apiKey = cfg.Embedding.APIKey
	}
	return &SiliconFlowReranker{
		cli: &OpenAIClient{
			httpClient: &http.Client{},
			timeout:    time.Duration(cfg.LLM.TimeoutSeconds) * time.Second,
			maxRetries: cfg.LLM.MaxRetry,
			cb:         NewCircuitBreaker(CircuitBreakerConfig{}),
		},
		baseURL: baseURL,
		apiKey:  apiKey,
		model:   cfg.Retrieval.RerankModel,
	}
}

// GetReranker 返回全局重排序器（惰性初始化）。
func GetReranker() Reranker {
	if reranker == nil {
		reranker = NewSiliconFlowReranker()
	}
	return reranker
}

func (r *SiliconFlowReranker) Rerank(ctx context.Context, query string, docs []string, topN int) ([]RerankScore, error) {
	if len(docs) == 0 {
		return nil, nil
	}
	if r.baseURL == "" || r.apiKey == "" {
		return nil, ErrRerankNotConfigured
	}
	if topN <= 0 || topN > len(docs) {
		topN = len(docs)
	}
	payload, err := json.Marshal(map[string]interface{}{
		"model":     r.model,
		"query":     query,
		"documents": docs,
		"top_n":     topN,
	})
	if err != nil {
		return nil, fmt.Errorf("rerank 请求构造失败: %w", err)
	}
	url := strings.TrimRight(r.baseURL, "/") + "/rerank"
	body, err := r.cli.doPost(ctx, url, r.apiKey, payload)
	if err != nil {
		return nil, fmt.Errorf("rerank 请求失败(model=%s): %w", r.model, err)
	}
	var resp struct {
		Results []struct {
			Index          int     `json:"index"`
			RelevanceScore float32 `json:"relevance_score"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("rerank 响应解析失败: %w", err)
	}
	out := make([]RerankScore, 0, len(resp.Results))
	for _, rr := range resp.Results {
		out = append(out, RerankScore{Index: rr.Index, Score: rr.RelevanceScore})
	}
	return out, nil
}
