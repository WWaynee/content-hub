package model

import (
	"time"

	"gorm.io/gorm"
)

// QASession 知识库问答会话（独立于工作区会话）。
type QASession struct {
	ID          uint64         `gorm:"column:id;primaryKey;autoIncrement"`
	TenantID    uint64         `gorm:"column:tenant_id;not null"`
	UserID      uint64         `gorm:"column:user_id;not null"`
	Title       string         `gorm:"column:title;size:256"`
	CreatedAt   time.Time      `gorm:"column:created_at;type:datetime(3)"`
	UpdatedAt   time.Time      `gorm:"column:updated_at;type:datetime(3)"`
	DeletedAt   gorm.DeletedAt `gorm:"column:deleted_at;type:datetime(3);index"`
}

func (QASession) TableName() string { return "qa_sessions" }

// QAMessage 问答消息。
type QAMessage struct {
	ID        uint64    `gorm:"column:id;primaryKey;autoIncrement"`
	SessionID uint64    `gorm:"column:session_id;not null"`
	TenantID  uint64    `gorm:"column:tenant_id;not null"`
	UserID    uint64    `gorm:"column:user_id;not null"`
	Role      string    `gorm:"column:role;size:16;not null"` // user/assistant
	Content   string    `gorm:"column:content;type:text"`
	TraceID   string    `gorm:"column:trace_id;size:128"`
	CreatedAt time.Time `gorm:"column:created_at;type:datetime(3)"`
}

func (QAMessage) TableName() string { return "qa_messages" }
