package llmclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// JSONFormatError 表示 LLM 返回内容在「简单修复 + 重试一次」后仍无法解析为合法 JSON。
type JSONFormatError struct {
	Raw string
}

func (e *JSONFormatError) Error() string {
	return fmt.Sprintf("LLM 返回无法解析为合法 JSON（重试后仍失败）: %s", e.Raw)
}

// ChatWithJSON 发送对话并要求严格 JSON，解析进 target。
// 容错：注入"只返回 JSON"提示 → 直接解析 → 简单修复（去代码块/截取对象/补括号）→ 重试一次。
func (c *OpenAIClient) ChatWithJSON(ctx context.Context, messages []ChatMessage, target interface{}) error {
	sys := []ChatMessage{{Role: "system", Content: "你必须只返回严格的合法 JSON 对象，不要任何解释文字、不要 Markdown 代码块、不要前后缀。"}}
	messages = append(sys, messages...)

	if err := c.callAndRepair(ctx, messages, target); err == nil {
		return nil
	} else if !isJSONFormatError(err) {
		return err
	}

	// 格式错误 → 重试一次
	messages = append(messages, ChatMessage{Role: "system", Content: "你上次返回的不是合法 JSON，请重新只返回一个合法 JSON 对象。"})
	return c.callAndRepair(ctx, messages, target)
}

func (c *OpenAIClient) callAndRepair(ctx context.Context, messages []ChatMessage, target interface{}) error {
	content, err := c.Chat(ctx, messages)
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(content), target); err == nil {
		return nil
	}
	fixed := normalizeJSON(content)
	if err := json.Unmarshal([]byte(fixed), target); err != nil {
		return &JSONFormatError{Raw: content}
	}
	return nil
}

func isJSONFormatError(err error) bool {
	var jerr *JSONFormatError
	return errors.As(err, &jerr)
}

// normalizeJSON 剥离代码块/多余文字，补齐括号，还原为可解析 JSON。
func normalizeJSON(raw string) string {
	s := strings.TrimSpace(raw)
	// 去 ``` 代码块
	if strings.HasPrefix(s, "```") {
		if idx := strings.Index(s, "\n"); idx != -1 {
			s = s[idx+1:]
		} else {
			s = strings.TrimPrefix(s, "```")
		}
		s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "```"))
	}
	first := strings.Index(s, "{")
	last := strings.LastIndex(s, "}")
	if first != -1 && last != -1 && last > first {
		s = s[first : last+1]
	}
	opens := strings.Count(s, "{")
	closes := strings.Count(s, "}")
	if opens > closes {
		s += strings.Repeat("}", opens-closes)
	}
	return strings.TrimSpace(s)
}
