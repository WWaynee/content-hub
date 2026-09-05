package service

import (
	"context"
	"fmt"

	"github.com/WWaynee/content-hub/agent"
	"github.com/WWaynee/content-hub/agent/dialogue"
	"github.com/WWaynee/content-hub/llmclient"
	"github.com/WWaynee/content-hub/storage"
	"github.com/WWaynee/content-hub/storage/model"
)

// Dispatcher 对话派发器：对话 agent 解析 → 逐 action 执行（写回 DB）。
type Dispatcher struct {
	dialogue *dialogue.Agent
}

// NewDispatcher 构造。
func NewDispatcher() *Dispatcher {
	return &Dispatcher{dialogue: dialogue.New(llmclient.NewClient())}
}

// DispatchResult 一次对话派发的结果（逐 action 明细）。
type DispatchResult struct {
	Plan    *agent.DialoguePlan
	Results []ActionResult
}

// ActionResult 单个 action 的执行结果。
//   - Message 保留底层/内部文案(可溯源)；HumanText 给用户看的一句话(不带 tool 名)；
//   - Debug 供日志/溯源；前端展示 HumanText+Success(✓/✕)，不再把 tool:…(msg) 怼给用户(W2)。
type ActionResult struct {
	Tool      string `json:"tool"`
	Success   bool   `json:"success"`
	Message   string `json:"message,omitempty"`    // 内部/技术文案
	HumanText string `json:"human_text,omitempty"` // 人话
	Debug     string `json:"debug,omitempty"`      // 内部细节（溯源用）
}

// ProcessChat 处理一条工作区对话消息：解析意图 → 逐 action 执行 → 落会话消息。
// tenantID/userID/workspaceID：当前上下文。
// 返回派发结果（含每步成败）。
func (d *Dispatcher) ProcessChat(ctx context.Context, tenantID, userID, workspaceID uint64, userMessage, targetType string, targetRef uint64) (*DispatchResult, error) {
	// 1. 确保会话
	conv, err := EnsureConversation(ctx, tenantID, workspaceID, userID)
	if err != nil {
		return nil, err
	}
	// 落用户消息
	if err := AppendUserMessage(ctx, conv.ID, tenantID, userID, userMessage, targetType, targetRef, ""); err != nil {
		return nil, err
	}

	// 2. 对话 agent 解析意图 → DialoguePlan
	req, err := storage.GetRequirementByWorkspace(ctx, tenantID, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("需求单不存在")
	}
	plan, err := d.dialogue.Parse(ctx, userMessage, "工作区需求单/稿件")
	if err != nil {
		return nil, fmt.Errorf("解析对话意图失败: %w", err)
	}

	// 落 plan 消息
	if err := AppendPlanMessage(ctx, conv.ID, tenantID, userID, plan, ""); err != nil {
		return nil, err
	}

	// 3. 逐 action 执行（不阻断后续；记录每步结果）
	res := &DispatchResult{Plan: plan}
	for _, ac := range plan.Actions {
		ar := d.execAction(ctx, tenantID, userID, workspaceID, req, ac)
		ar.HumanText = humanizeAction(ac, ar)
		ar.Debug = ar.Message
		res.Results = append(res.Results, ar)
	}
	return res, nil
}

func (d *Dispatcher) execAction(ctx context.Context, tenantID, userID, workspaceID uint64, req *model.Requirement, ac agent.DialogueAction) ActionResult {
	switch ac.Tool {
	case "update_requirement_field":
		if _, err := UpdateRequirementField(ctx, req.ID, ac.Field, ac.FieldValue); err != nil {
			return ActionResult{Tool: ac.Tool, Success: false, Message: err.Error()}
		}
		return ActionResult{Tool: ac.Tool, Success: true, Message: "已更新需求单字段 " + ac.Field}

	case "request_retrieval":
		// 补检索 + 落检索快照（供惰性失效判定 / 证据追溯）
		hits, err := SearchKbaseSentences(ctx, tenantID, ac.RetrievalQuery)
		if err != nil {
			return ActionResult{Tool: ac.Tool, Success: false, Message: err.Error()}
		}
		// 落快照（requirementVersion 用当前需求单 version）
		if _, berr := PersistRetrievalBatch(ctx, tenantID, workspaceID, req.ID, req.Version, []string{ac.RetrievalQuery}, hits); berr != nil {
			return ActionResult{Tool: ac.Tool, Success: false, Message: berr.Error()}
		}
		return ActionResult{Tool: ac.Tool, Success: true, Message: fmt.Sprintf("补检索命中 %d 条，已记录检索快照", len(hits))}

	case "revise_article_sentence":
		// 完整句子级修订并记录为 revision run：LLM 重写目标句 + 重检测 + 落新快照
		if _, _, err := RunRevision(ctx, tenantID, userID, workspaceID, ac.TargetSentenceIndex, ac.Instruction); err != nil {
			return ActionResult{Tool: ac.Tool, Success: false, Message: err.Error()}
		}
		return ActionResult{Tool: ac.Tool, Success: true, Message: fmt.Sprintf("已修订第 %d 句", ac.TargetSentenceIndex)}

	case "append_article_content":
		// 追加段落并记录为 append run
		if _, _, err := RunAppend(ctx, tenantID, userID, workspaceID, ac.Instruction); err != nil {
			return ActionResult{Tool: ac.Tool, Success: false, Message: err.Error()}
		}
		return ActionResult{Tool: ac.Tool, Success: true, Message: "已追加段落"}

	default:
		return ActionResult{Tool: ac.Tool, Success: false, Message: "未知工具 " + ac.Tool}
	}
}

// fieldCN 白名单内需求单字段的中文人话标签（对外展示，避免直接暴露 style_tone 之类字段名）。
func fieldCN(f string) string {
	switch f {
	case "style_tone":
		return "发文基调"
	case "style_emotion":
		return "感情色彩"
	case "style_audience":
		return "目标受众"
	case "style_purpose":
		return "发文目的"
	case "style_taboo":
		return "禁忌/约束"
	case "style_subject":
		return "发文主题"
	case "chapter_requirement":
		return "章节要求"
	case "word_count":
		return "字数"
	default:
		if f == "" {
			return "需求单"
		}
		return f
	}
}

// humanizeAction 把某个 action 的执行结果转成不暴露 tool 名的人话（W2）。
// 同一措辞无论成败都用；成功补“已完成”，失败补“未能完成 + 一句可读的下一步”。
func humanizeAction(ac agent.DialogueAction, ar ActionResult) string {
	var intent string
	switch ac.Tool {
	case "update_requirement_field":
		if field := ac.Field; field != "" {
			intent = fmt.Sprintf("把你的要求写进需求单的「%s」", fieldCN(field))
		} else {
			intent = "把你的要求写进需求单"
		}
	case "request_retrieval":
		intent = "帮你在知识库里再检索一遍并记录结果"
	case "append_article_content":
		intent = "把你说的内容补进稿件"
	case "revise_article_sentence":
		intent = "按你说的说法改写稿件里的文字"
	default:
		intent = fmt.Sprintf("执行 %q 这步操作", ac.Tool)
	}
	if ar.Success {
		return "✓ " + intent + "，已完成。"
	}
	return "未能" + intent + "。可以换个说法再说一次，或到「逐句编辑」/需求单里手动处理。"
}
