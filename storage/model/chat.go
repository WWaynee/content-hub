package model

import (
	"time"

	"gorm.io/gorm"
)

// Conversation 会话（工作区一份）
type Conversation struct {
	ID          uint64         `gorm:"column:id;primaryKey;autoIncrement"`
	WorkspaceID uint64         `gorm:"column:workspace_id;not null"`
	TenantID    uint64         `gorm:"column:tenant_id;not null"`
	OwnerUserID uint64         `gorm:"column:owner_user_id;not null"`
	CreatedAt   time.Time      `gorm:"column:created_at;type:datetime(3)"`
	UpdatedAt   time.Time      `gorm:"column:updated_at;type:datetime(3)"`
	DeletedAt   gorm.DeletedAt `gorm:"column:deleted_at;type:datetime(3);index"`
}

func (Conversation) TableName() string { return "conversations" }

// ConversationMessage 会话消息（带 target 锚点）
type ConversationMessage struct {
	ID             uint64    `gorm:"column:id;primaryKey;autoIncrement"`
	ConversationID uint64    `gorm:"column:conversation_id;not null"`
	TenantID       uint64    `gorm:"column:tenant_id;not null"`
	OwnerUserID    uint64    `gorm:"column:owner_user_id;not null"`
	Role           string    `gorm:"column:role;size:16;not null"` // user/assistant/tool
	Kind           string    `gorm:"column:kind;size:16;not null;default:'question'"` // question/answer/tool_call/tool_result/system
	Content        string    `gorm:"column:content;type:text"`
	TargetType     string    `gorm:"column:target_type;size:32;not null;default:'none'"` // none/sentence/paragraph/requirement_field
	TargetRef      uint64    `gorm:"column:target_ref;not null;default:0"`
	TraceID        string    `gorm:"column:trace_id;size:128"`
	CreatedAt      time.Time `gorm:"column:created_at;type:datetime(3)"`
}

func (ConversationMessage) TableName() string { return "conversation_messages" }
