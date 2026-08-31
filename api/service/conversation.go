package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/WWaynee/content-hub/agent"
	"github.com/WWaynee/content-hub/storage"
	"github.com/WWaynee/content-hub/storage/model"
)

// 会话业务层（阶段私有 session，存 action plan JSON）。

// EnsureConversation 确保工作区有会话（没有则创建），返回会话。
func EnsureConversation(ctx context.Context, tenantID, workspaceID, ownerUserID uint64) (*model.Conversation, error) {
	c, err := storage.GetConversationByWorkspace(ctx, tenantID, workspaceID)
	if err == nil {
		return c, nil
	}
	c = &model.Conversation{
		WorkspaceID: workspaceID,
		TenantID:    tenantID,
		OwnerUserID: ownerUserID,
	}
	if err := storage.CreateConversation(ctx, c); err != nil {
		return nil, fmt.Errorf("创建会话失败: %w", err)
	}
	return c, nil
}

// AppendUserMessage 追加一条用户消息（含可选 target 锚点）。
func AppendUserMessage(ctx context.Context, convID, tenantID, userID uint64, content string, targetType string, targetRef uint64, traceID string) error {
	m := &model.ConversationMessage{
		ConversationID: convID,
		TenantID:       tenantID,
		OwnerUserID:    userID,
		Role:           "user",
		Kind:           "question",
		Content:        content,
		TargetType:     targetType,
		TargetRef:      targetRef,
		TraceID:        traceID,
	}
	return storage.AppendConversationMessage(ctx, m)
}

// AppendPlanMessage 追加一条 assistant 消息（存 action plan JSON）。
func AppendPlanMessage(ctx context.Context, convID, tenantID, userID uint64, plan *agent.DialoguePlan, traceID string) error {
	b, _ := json.Marshal(plan)
	m := &model.ConversationMessage{
		ConversationID: convID,
		TenantID:       tenantID,
		OwnerUserID:    userID,
		Role:           "assistant",
		Kind:           "tool_call",
		Content:        string(b),
		TraceID:        traceID,
	}
	return storage.AppendConversationMessage(ctx, m)
}
