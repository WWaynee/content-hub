// Package dialogue 实现对话 agent（统一入口）：把用户自然语言对话转成
// 结构化动作计划（DialoguePlan，可含多个动作），供调度层机检后派发执行。
package dialogue

import (
	"context"
	"fmt"
	"strings"

	"github.com/WWaynee/content-hub/agent"
	"github.com/WWaynee/content-hub/agent/schema"
	"github.com/WWaynee/content-hub/llmclient"
)

// Agent 对话 agent（统一入口）。
type Agent struct {
	llm llmclient.Client
}

// New 构造对话 agent。
func New(llm llmclient.Client) *Agent { return &Agent{llm: llm} }

// planOutput LLM 输出的动作计划（与 agent.DialoguePlan 对齐）。
type planOutput struct {
	Actions []agent.DialogueAction `json:"actions"`
}

// Parse 把用户自然语言转成结构化动作计划（DialoguePlan）。
// 返回的动作计划已经过 schema.Validate 机检，不合法的返回错误。
func (a *Agent) Parse(ctx context.Context, userMessage string, contextHint string) (*agent.DialoguePlan, error) {
	prompt := buildPrompt(userMessage, contextHint)

	var out planOutput
	if err := a.llm.ChatWithJSON(ctx, []llmclient.ChatMessage{{Role: "user", Content: prompt}}, &out); err != nil {
		return nil, fmt.Errorf("解析对话意图失败: %w", err)
	}

	plan := &agent.DialoguePlan{Actions: out.Actions}
	// 机检：白名单 + 字段合法性
	if err := schema.Validate(plan); err != nil {
		return nil, fmt.Errorf("动作计划机检失败: %w", err)
	}
	return plan, nil
}

func buildPrompt(userMessage, contextHint string) string {
	var sb strings.Builder
	sb.WriteString("你是政企内容平台的对话助手。请分析用户意图，把一句话拆解成【一个或多个原子动作】，输出结构化 JSON。\n\n")
	if contextHint != "" {
		sb.WriteString("【当前上下文】\n" + contextHint + "\n\n")
	}
	sb.WriteString("【可用工具（tool 字段）】\n")
	sb.WriteString(`- update_requirement_field：修改需求单字段。 field 只能是白名单内的：style_tone/style_emotion/style_audience/style_purpose/style_taboo/style_subject/chapter_requirement
- request_retrieval：请求补充检索。 retrieval_query 填要检索的主题
- append_article_content：在稿件末尾新增内容。 instruction 填新增内容要求，position 填 last
- revise_article_sentence：改写稿件某个句子。 target_sentence_index 填句子序号(0起)，instruction 填修改要求
`)
	sb.WriteString("\n【用户输入】\n" + userMessage + "\n\n")
	sb.WriteString(`只返回 JSON：{"actions":[{"tool":"...","field":"...","field_value":"..."}]}
注意：
1) 一句用户输入可以包含多个动作，放进 actions 数组。
2) 字数要求、勾选文档范围这类核心字段不可修改，不要生成 update_requirement_field 指向它们。
3) 一条 action 只对应一个原子操作。
4) 当新增/改写的内容涉及"需要查资料才能知道的事实数据"（如昨天气温、某政策条款），必须先产出一个 request_retrieval 动作（retrieval_query 说明要查什么），再产出对应的稿件增补/改写动作；不要把"查资料"隐含在稿件动作里。
5) 请求检索和改写稿件是两个独立的动作，不要合并。`)
	return sb.String()
}
