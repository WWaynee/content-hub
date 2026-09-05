package llmclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/WWaynee/content-hub/config"
	"github.com/WWaynee/content-hub/observability"
)

// Client LLM 客户端接口（业务层只依赖此接口）。
type Client interface {
	Embed(ctx context.Context, input string) ([]float32, error)
	EmbedBatch(ctx context.Context, inputs []string) ([][]float32, error)
	Chat(ctx context.Context, messages []ChatMessage) (string, error)
	// ChatWithJSON 发送对话并要求严格 JSON，解析进 target（多 agent 结构化输出用）。
	ChatWithJSON(ctx context.Context, messages []ChatMessage, target interface{}) error
}

// ChatMessage 对话消息。
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// llmPerKB：长 prompt 每 1KB 请求体追加的推理超时余量。
// deepseek-v4-flash 等推理模型对长上下文的 thinking 时长增长明显，实测约 58KB→35.6s，
// 固定超时会让大 prompt 必然超时，这里按负载补足。
const llmPerKB = time.Duration(700) * time.Millisecond

// maxLLMTimeout 单次 LLM 请求的最长预算（封顶防失控/DDOS 式自我放大）。
const maxLLMTimeout = 10 * time.Minute

// OpenAIClient OpenAI 兼容实现。
type OpenAIClient struct {
	chatModel    string
	embedModel   string
	embedBaseURL string
	embedAPIKey  string
	httpClient   *http.Client
	timeout      time.Duration
	maxRetries   int
	cb           *CircuitBreaker
}

// NewClient 构造客户端（对话走 LLM 配置，向量走 Embedding 配置）。
func NewClient() Client {
	cfg := config.Get()
	embedBase := cfg.Embedding.BaseURL
	if embedBase == "" {
		embedBase = cfg.LLM.BaseURL
	}
	embedKey := cfg.Embedding.APIKey
	if embedKey == "" {
		embedKey = cfg.LLM.APIKey
	}
	return &OpenAIClient{
		chatModel:    cfg.LLM.ChatModel,
		embedModel:   cfg.Embedding.Model,
		embedBaseURL: embedBase,
		embedAPIKey:  embedKey,
		httpClient:   &http.Client{},
		timeout:      time.Duration(cfg.LLM.TimeoutSeconds) * time.Second,
		maxRetries:   cfg.LLM.MaxRetry,
		cb:           NewCircuitBreaker(CircuitBreakerConfig{}),
	}
}

// doPost 发送 POST 请求 + 简单重试。
//
// 超时预算说明（修复"生成失败: chat 请求失败: context deadline exceeded"）：
//   - 原来所有重试共享同一个 deadline，等于把 maxRetries 次尝试 + 退避全部挤在
//     一个 LLM_TIMEOUT_SECONDS 里——对大 prompt（写作/事实校验会把全部证据塞进
//     一次推理模型的请求）单次本就可能超过 base，再重试只会越压越小直至超时（
//     这正是"事实断言提取失败: chat 请求失败: context deadline exceeded"的成因）。
//   - 现在改为：根据请求负载大小算出单次预算 c.timeoutBudget(len(payload))，
//     每次尝试独立拥有完整预算；仅当外层 ctx 已带更紧的 deadline 时以它为准
//     （无法放大外部约束，退避也不吞噬其它尝试的预算）。
func (c *OpenAIClient) doPost(ctx context.Context, url, apiKey string, payload []byte) ([]byte, error) {
	// 计算"每一次尝试"的独立超时预算（按负载放大）；若外层 ctx 自身已带更紧的
	// deadline，则无论如何无法超过它，直接沿用外层 deadline。
	ownBudget := c.timeoutBudget(len(payload))
	if ext, has := ctx.Deadline(); has {
		if remain := time.Until(ext); remain < ownBudget {
			ownBudget = remain
		}
	}

	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if !c.cb.allow() {
			return nil, ErrCircuitOpen
		}
		// 每次尝试独立分配完整预算：上一次尝试超时/失败不消耗下一次尝试的预算。
		attemptCtx := ctx
		var cancel context.CancelFunc
		if _, has := ctx.Deadline(); !has {
			attemptCtx, cancel = context.WithTimeout(ctx, ownBudget)
		} else {
			cancel = func() {}
		}
		body, err := c.singlePost(attemptCtx, url, apiKey, payload)
		cancel()
		c.cb.record(err == nil)
		if err == nil {
			return body, nil
		}
		lastErr = err
		// 4xx 不重试
		var apiErr *APIError
		if errors.As(err, &apiErr) && !apiErr.Retryable {
			return nil, err
		}
		if attempt >= c.maxRetries {
			return nil, err
		}
		// 退避发生在本次 attemptCtx 之外，不吞噬同一 context 的后续预算。
		// 若外层 deadline 已到则即便退避结束也不该再发（下次循环 + WithTimeout 仍由外层兜住）。
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoffDelay(attempt)):
		}
	}
	return nil, lastErr
}

// backoffDelay 指数退避：1s, 2s, 4s ...（按尝试次数）。
func backoffDelay(attempt int) time.Duration {
	return time.Duration(1<<uint(attempt)) * time.Second
}

// timeoutBudget 依据请求体大小给出单次尝试的独立超时预算。
// 推理模型对长上下文耗时增长明显（thinking tokens），固定 timeout 会让大 prompt 必然
// 超时，故预算 = 基础 timeout + 按每 KB 负载叠加的推理余量，并封顶防失控。
func (c *OpenAIClient) timeoutBudget(payloadBytes int) time.Duration {
	budget := c.timeout
	extraKB := payloadBytes >> 10
	if extraKB > 0 {
		budget += time.Duration(extraKB) * llmPerKB
	}
	if budget > maxLLMTimeout {
		budget = maxLLMTimeout
	}
	return budget
}

func (c *OpenAIClient) singlePost(ctx context.Context, url, apiKey string, payload []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, &APIError{StatusCode: 0, Body: err.Error(), Retryable: true}
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &APIError{StatusCode: resp.StatusCode, Body: "read: " + err.Error(), Retryable: true}
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return b, nil
	}
	return nil, &APIError{StatusCode: resp.StatusCode, Body: string(b), Retryable: resp.StatusCode >= 500}
}

// APIError HTTP 层错误。
type APIError struct {
	StatusCode int
	Body       string
	Retryable  bool
}

func (e *APIError) Error() string { return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Body) }

func (c *OpenAIClient) Embed(ctx context.Context, input string) ([]float32, error) {
	vecs, err := c.EmbedBatch(ctx, []string{input})
	if err != nil {
		return nil, err
	}
	if len(vecs) == 0 {
		return nil, fmt.Errorf("embedding 返回空向量")
	}
	return vecs[0], nil
}

func (c *OpenAIClient) EmbedBatch(ctx context.Context, inputs []string) ([][]float32, error) {
	if len(inputs) == 0 {
		return [][]float32{}, nil
	}
	payload, _ := json.Marshal(map[string]interface{}{
		"model": c.embedModel,
		"input": inputs,
	})
	url := c.embedBaseURL + "/embeddings"
	body, err := c.doPost(ctx, url, c.embedAPIKey, payload)
	if err != nil {
		return nil, fmt.Errorf("embedding 请求失败: %w", err)
	}
	var resp struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("embedding 响应解析失败: %w", err)
	}
	if len(resp.Data) != len(inputs) {
		return nil, fmt.Errorf("embedding 返回数量不符: 期望 %d 实际 %d", len(inputs), len(resp.Data))
	}
	vecs := make([][]float32, len(resp.Data))
	for i, d := range resp.Data {
		vecs[i] = d.Embedding
	}
	return vecs, nil
}

func (c *OpenAIClient) Chat(ctx context.Context, messages []ChatMessage) (string, error) {
	payload, _ := json.Marshal(map[string]interface{}{
		"model":    c.chatModel,
		"messages": messages,
		"stream":   false,
	})
	url := config.Get().LLM.BaseURL + "/chat/completions"
	body, err := c.doPost(ctx, url, config.Get().LLM.APIKey, payload)
	if err != nil {
		observability.IncLLMCall(c.chatModel, false)
		return "", fmt.Errorf("chat 请求失败: %w", err)
	}
	var resp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		observability.IncLLMCall(c.chatModel, false)
		return "", fmt.Errorf("chat 响应解析失败: %w", err)
	}
	if len(resp.Choices) == 0 {
		observability.IncLLMCall(c.chatModel, false)
		return "", fmt.Errorf("chat 响应无 choices")
	}
	observability.IncLLMCall(c.chatModel, true)
	return resp.Choices[0].Message.Content, nil
}
