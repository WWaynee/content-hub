// Package qabot 实现知识库问答 agent（纯问答，不做稿件）。
//
// 设计参照前身系统 agent-platform 的 ReAct 引擎能力，针对「纯问答」形态做轻量化：
//   - 引入 System Prompt：明确角色与「检索不到必须如实回答」的硬规则；
//   - 支持多轮检索：同一问题可多次（最多 MaxRounds 次）发起不同角度的检索，
//     覆盖"一个问题需要跨文档/多主题收集"的场景；
//   - 检索次数上限：MaxRounds 限制一轮提问内最多发起几次检索，防止无限循环；
//   - 每次检索结果（连同来源文档）作为观察回喂给 LLM，让 LLM 判断是继续补检
//     还是综合已收集资料直接作答；某个角度检索不到时，把"未找到"如实回喂，
//     由 LLM 决定如实告知用户，而非硬编码一句固定话术。
package qabot

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/WWaynee/content-hub/llmclient"
)

// Hit 一次检索命中的原文片段（含来源，供跨文档组织与溯源）。
type Hit struct {
	Content  string // 切片原文，不加工
	FileName string // 来源文档名（可空）
}

// Retriever 检索接口（由 service 层注入，避免 agent 反向依赖 service）。
type Retriever interface {
	// Retrieve 检索知识库，返回命中的原文片段（含来源）。
	Retrieve(ctx context.Context, tenantID uint64, query string) ([]Hit, error)
}

// Config 回答参数。
type Config struct {
	// MaxRounds 一轮提问内最多允许的检索轮数（含 LLM 决策往返）。
	// 0 时用默认值 4。达到上限仍未给最终回答则由兜底逻辑收尾。
	MaxRounds int
}

// Agent 知识库问答 agent。
type Agent struct {
	llm       llmclient.Client
	retriever Retriever
	config    Config
}

// New 构造（retriever 由上层注入）。
func New(llm llmclient.Client, retriever Retriever, cfg Config) *Agent {
	if retriever == nil {
		retriever = &noopRetriever{}
	}
	if cfg.MaxRounds <= 0 {
		cfg.MaxRounds = 4
	}
	return &Agent{llm: llm, retriever: retriever, config: cfg}
}

// AnswerResult 一次问答的结果。
type AnswerResult struct {
	Answer      string   // 回答
	HasHit      bool     // 是否至少检索到一次资料（供调用方判断命中态）
	SourceTexts []string // 命中的原文片段（供前端展示/溯源，去重）
}

// systemPrompt 明确角色 + 多轮检索规则 + "检索不到必须如实回答" + 输出格式。
// 输出格式用 JSON 便于程序稳定解析；不允许编造知识。
const systemPrompt = `你是企业的内部知识库问答助手。职责是：基于企业知识库中的资料，如实回答用户的问题；
对于知识库覆盖不到的问题，必须如实说明"知识库中未检索到相关资料"，绝不编造、绝不臆测。

【检索方式】
1. 回答前应当先检索知识库。你可以分多轮、从多个不同的切入点（关键词/角度）分别检索，
   以覆盖"一个问题需要查阅多份文档/多个主题才能收集完整"的情况。
2. 每轮只做一件事：要么发起一次检索，要么给出最终回答。
3. 如果某一轮检索没有结果，不代表整个知识库都没有——你可以换一个角度或关键词再检索一到两次，
   确认确实覆盖不到后，再如实告知用户"知识库中未检索到相关资料"。
4. 当你认为已收集到足够依据，或确认知识库中确实没有相关内容时，给出最终回答。

【回答要求】
- 只依据检索到的知识库资料回答；引用要点尽量指出来源文档名。
- 回答用简洁的中文，直接给结论。

【输出格式】
你必须严格只输出一条合法的 JSON 对象，不要任何解释文字、Markdown 代码块或前后缀：
- 需要发起检索时输出：{"action":"retrieve","query":"你的检索词"}
- 可以给出最终回答时输出：{"action":"final_answer","answer":"你的回答"}`

// Answer 回答用户问题：多轮检索 + 综合作答。
func (a *Agent) Answer(ctx context.Context, tenantID uint64, question string) (*AnswerResult, error) {
	messages := []llmclient.ChatMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: question},
	}

	var collected []Hit
	seen := map[string]bool{}
	addHit := func(h Hit) {
		if h.Content == "" || seen[h.Content] {
			return
		}
		seen[h.Content] = true
		collected = append(collected, h)
	}

	for round := 0; round < a.config.MaxRounds; round++ {
		raw, err := a.llm.Chat(ctx, messages)
		if err != nil {
			return nil, fmt.Errorf("回答生成失败: %w", err)
		}
		act, perr := parseAction(raw)
		if perr != nil {
			// 解析失败：回喂纠错提示，让 LLM 重发（占一轮）
			messages = append(messages,
				llmclient.ChatMessage{Role: "user", Content: "你的输出不是合法的 {action, ...} JSON，请严格按格式只输出一条 JSON。错误: " + perr.Error()},
			)
			continue
		}

		switch act.Action {
		case "final_answer":
			return &AnswerResult{
				Answer:      strings.TrimSpace(act.Answer),
				HasHit:      len(collected) > 0,
				SourceTexts: collectTexts(collected),
			}, nil

		case "retrieve":
			query := strings.TrimSpace(act.Query)
			if query == "" {
				messages = append(messages,
					llmclient.ChatMessage{Role: "user", Content: "retrieve 动作的 query 不能为空，请重新输出。"},
				)
				continue
			}
			hits, err := a.retriever.Retrieve(ctx, tenantID, query)
			if err != nil {
				return nil, fmt.Errorf("检索失败: %w", err)
			}
			if len(hits) == 0 {
				// 如实回喂"该角度未检索到"，由 LLM 决定是换角度再试还是如实告知
				messages = append(messages,
					llmclient.ChatMessage{Role: "assistant", Content: raw},
					llmclient.ChatMessage{Role: "user", Content: fmt.Sprintf("检索 %q 没有任何结果：知识库中没有与这个角度相关的资料。你可以换一个切入点再检索，也可以直接给出最终回答（若确认无法覆盖，请如实告知用户'未检索到相关资料'）。", query)},
				)
				continue
			}
			for _, h := range hits {
				addHit(h)
			}
			messages = append(messages,
				llmclient.ChatMessage{Role: "assistant", Content: raw},
				llmclient.ChatMessage{Role: "user", Content: buildObservation(hits)},
			)
			continue

		default:
			// 未知 action：纠错
			messages = append(messages,
				llmclient.ChatMessage{Role: "user", Content: fmt.Sprintf("未知 action: %q，只允许 retrieve 或 final_answer。请重新输出。", act.Action)},
			)
			continue
		}
	}

	// 达到最大检索轮次仍未给最终回答：若已收集到资料，让 LLM 综合作答，否则如实说未检索到。
	if len(collected) > 0 {
		messages = append(messages,
			llmclient.ChatMessage{Role: "user", Content: "检索轮数已达上限，请立即基于以上已检索到的资料给出最终回答，不要再次检索。"},
		)
		raw, err := a.llm.Chat(ctx, messages)
		if err != nil {
			return nil, fmt.Errorf("回答生成失败: %w", err)
		}
		if act, perr := parseAction(raw); perr == nil && act.Action == "final_answer" {
			return &AnswerResult{Answer: strings.TrimSpace(act.Answer), HasHit: true, SourceTexts: collectTexts(collected)}, nil
		}
		return &AnswerResult{Answer: strings.TrimSpace(raw), HasHit: true, SourceTexts: collectTexts(collected)}, nil
	}

	return &AnswerResult{Answer: "未检索到相关资料。", HasHit: false}, nil
}

// buildObservation 把一次检索命中的片段（含来源文档）格式化成观察结果回喂 LLM。
func buildObservation(hits []Hit) string {
	var b strings.Builder
	fmt.Fprintf(&b, "检索到以下资料（共 %d 条）：\n", len(hits))
	for i, h := range hits {
		src := h.FileName
		if src == "" {
			src = "未知文档"
		}
		fmt.Fprintf(&b, "[%d] 来源：%s\n内容：%s\n", i+1, src, h.Content)
	}
	b.WriteString("请继续：可再换角度检索，或综合以上资料给出最终回答。")
	return b.String()
}

// collectTexts 汇总命中原文字段，按出现顺序去重。
func collectTexts(hits []Hit) []string {
	out := make([]string, 0, len(hits))
	seen := map[string]bool{}
	for _, h := range hits {
		if h.Content == "" || seen[h.Content] {
			continue
		}
		seen[h.Content] = true
		out = append(out, h.Content)
	}
	return out
}

// llmAction LLM 输出的结构化动作。
type llmAction struct {
	Action string `json:"action"`
	Query  string `json:"query"`
	Answer string `json:"answer"`
}

// parseAction 解析 LLM 输出为结构化动作。
func parseAction(raw string) (llmAction, error) {
	s := strings.TrimSpace(raw)
	// 容忍被 ``` 包裹
	if idx := strings.Index(s, "{"); idx >= 0 {
		s = s[idx:]
	}
	if idx := strings.LastIndex(s, "}"); idx >= 0 {
		s = s[:idx+1]
	}
	var a llmAction
	if err := json.Unmarshal([]byte(s), &a); err != nil {
		return a, fmt.Errorf("无法解析为 JSON: %v", err)
	}
	if a.Action == "" {
		return a, fmt.Errorf("缺少 action 字段")
	}
	return a, nil
}

type noopRetriever struct{}

func (n *noopRetriever) Retrieve(ctx context.Context, tenantID uint64, query string) ([]Hit, error) {
	return nil, nil
}
