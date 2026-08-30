// Package dialogue 实现需求对话 agent：把用户自然语言对话转成结构化操作
// （修改需求单字段 / 修订稿件句子）。
package dialogue

import (
	"context"
	"fmt"

	"github.com/WWaynee/content-hub/agent"
	"github.com/WWaynee/content-hub/llmclient"
)

// Agent 需求对话 agent。
type Agent struct {
	llm llmclient.Client
}

// New 构造对话 agent。
func New(llm llmclient.Client) *Agent { return &Agent{llm: llm} }

// Parse 把用户自然语言转成结构化操作。
func (a *Agent) Parse(ctx context.Context, userMessage string, contextHint string) (*agent.DialogueAction, error) {
	prompt := fmt.Sprintf(`你是政企内容平台的对话助手。请分析用户意图，输出结构化操作。

%s

用户输入：%s

只返回 JSON，格式（按意图选一种）：
1) 修改需求单字段：{"type":"update_requirement","field":"style_tone|style_emotion|style_audience|style_purpose|style_taboo|style_subject|chapter_requirement","field_value":"新值"}
2) 修订稿件：{"type":"revise_article","target_sentence_index":0,"instruction":"修改要求","needs_retrieval":false,"retrieval_query":""}

如果修订需要检索新资料，把 needs_retrieval 设为 true 并给出 retrieval_query。`, contextHint, userMessage)

	var out struct {
		Type               string `json:"type"`
		Field              string `json:"field"`
		FieldValue         string `json:"field_value"`
		TargetSentenceIndex int   `json:"target_sentence_index"`
		Instruction        string `json:"instruction"`
		NeedsRetrieval     bool   `json:"needs_retrieval"`
		RetrievalQuery     string `json:"retrieval_query"`
	}
	if err := a.llm.ChatWithJSON(ctx, []llmclient.ChatMessage{{Role: "user", Content: prompt}}, &out); err != nil {
		return nil, fmt.Errorf("解析对话意图失败: %w", err)
	}
	return &agent.DialogueAction{
		Type:                out.Type,
		Field:               out.Field,
		FieldValue:          out.FieldValue,
		TargetSentenceIndex: out.TargetSentenceIndex,
		Instruction:         out.Instruction,
		NeedsRetrieval:      out.NeedsRetrieval,
		RetrievalQuery:      out.RetrievalQuery,
	}, nil
}
