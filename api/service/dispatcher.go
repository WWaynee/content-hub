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
type ActionResult struct {
	Tool    string `json:"tool"`
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
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
