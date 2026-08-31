// Package qabot 实现知识库问答 agent（纯问答，不做稿件）。
// 检索知识库 → 有结果则基于原文回答，无结果则如实告知"未检索到相关资料"。
package qabot

import (
	"context"
	"fmt"
	"strings"

	"github.com/WWaynee/content-hub/llmclient"
)

// Retriever 检索接口（由 service 层注入，避免 agent 反向依赖 service）。
type Retriever interface {
	// Retrieve 检索知识库，返回命中的原文片段。
	Retrieve(ctx context.Context, tenantID uint64, query string) ([]string, error)
}

// Agent 知识库问答 agent。
type Agent struct {
	llm       llmclient.Client
	retriever Retriever
}

// New 构造（retriever 由上层注入）。
func New(llm llmclient.Client, retriever Retriever) *Agent {
	if retriever == nil {
		retriever = &noopRetriever{}
	}
	return &Agent{llm: llm, retriever: retriever}
}

// AnswerResult 一次问答的结果。
type AnswerResult struct {
	Answer      string   // 回答
	HasHit      bool     // 是否检索到资料
	SourceTexts []string // 命中的原文片段（供前端展示/溯源）
}

// Answer 回答用户问题：检索知识库 → 拼上下文 → LLM 回答。
func (a *Agent) Answer(ctx context.Context, tenantID uint64, question string) (*AnswerResult, error) {
	texts, err := a.retriever.Retrieve(ctx, tenantID, question)
	if err != nil {
		return nil, fmt.Errorf("检索失败: %w", err)
	}

	if len(texts) == 0 {
		return &AnswerResult{Answer: "未检索到相关资料。", HasHit: false}, nil
	}

	var sb strings.Builder
	sb.WriteString("根据以下知识库资料回答用户问题，不要编造资料中没有的内容。\n\n【资料】\n")
	for i, t := range texts {
		sb.WriteString(fmt.Sprintf("[%d] %s\n", i, t))
	}
	sb.WriteString("\n【用户问题】" + question + "\n\n请用简洁的中文回答。")

	answer, err := a.llm.Chat(ctx, []llmclient.ChatMessage{{Role: "user", Content: sb.String()}})
	if err != nil {
		return nil, fmt.Errorf("回答生成失败: %w", err)
	}

	return &AnswerResult{Answer: answer, HasHit: true, SourceTexts: texts}, nil
}

type noopRetriever struct{}

func (n *noopRetriever) Retrieve(ctx context.Context, tenantID uint64, query string) ([]string, error) {
	return nil, nil
}
