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
)

// Client LLM 客户端接口（业务层只依赖此接口）。
type Client interface {
	Embed(ctx context.Context, input string) ([]float32, error)
	EmbedBatch(ctx context.Context, inputs []string) ([][]float32, error)
	Chat(ctx context.Context, messages []ChatMessage) (string, error)
}

// ChatMessage 对话消息。
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// OpenAIClient OpenAI 兼容实现。
type OpenAIClient struct {
	chatModel     string
	embedModel    string
	embedBaseURL  string
	embedAPIKey   string
	httpClient    *http.Client
	timeout       time.Duration
	maxRetries    int
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
	}
}

// doPost 发送 POST 请求 + 简单重试。
func (c *OpenAIClient) doPost(ctx context.Context, url, apiKey string, payload []byte) ([]byte, error) {
	deadlineCtx := ctx
	if _, has := ctx.Deadline(); !has {
		var cancel context.CancelFunc
		deadlineCtx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}

	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		body, err := c.singlePost(deadlineCtx, url, apiKey, payload)
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
		select {
		case <-deadlineCtx.Done():
			return nil, deadlineCtx.Err()
		case <-time.After(time.Duration(1<<uint(attempt)) * time.Second):
		}
	}
	return nil, lastErr
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
		return "", fmt.Errorf("chat 响应解析失败: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("chat 响应无 choices")
	}
	return resp.Choices[0].Message.Content, nil
}
