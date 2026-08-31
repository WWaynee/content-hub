package storage

import (
	"context"

	"github.com/WWaynee/content-hub/storage/model"
)

// 会话存储层（conversations + conversation_messages，阶段私有 session）。

func CreateConversation(ctx context.Context, c *model.Conversation) error {
	return GetDB().WithContext(ctx).Create(c).Error
}

func GetConversationByWorkspace(ctx context.Context, tenantID, workspaceID uint64) (*model.Conversation, error) {
	var c model.Conversation
	if err := GetDB().WithContext(ctx).
		Where("tenant_id = ? AND workspace_id = ?", tenantID, workspaceID).
		First(&c).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

// AppendConversationMessage 追加一条会话消息（含 action plan JSON / target 锚点）。
func AppendConversationMessage(ctx context.Context, m *model.ConversationMessage) error {
	return GetDB().WithContext(ctx).Create(m).Error
}

// ListConversationMessages 列出某会话的全部消息（按时间正序）。
func ListConversationMessages(ctx context.Context, conversationID uint64) ([]model.ConversationMessage, error) {
	var list []model.ConversationMessage
	if err := GetDB().WithContext(ctx).
		Where("conversation_id = ?", conversationID).
		Order("id ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}
