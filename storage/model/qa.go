package model

import (
	"time"

	"gorm.io/gorm"
)

// QASession 知识库问答会话（独立于工作区会话）。
type QASession struct {
	ID        uint64         `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	TenantID  uint64         `gorm:"column:tenant_id;not null" json:"tenant_id"`
	UserID    uint64         `gorm:"column:user_id;not null" json:"user_id"`
	Title     string         `gorm:"column:title;size:256" json:"title"`
	CreatedAt time.Time      `gorm:"column:created_at;type:datetime(3)" json:"created_at"`
	UpdatedAt time.Time      `gorm:"column:updated_at;type:datetime(3)" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"column:deleted_at;type:datetime(3);index" json:"deleted_at,omitempty"`
}

func (QASession) TableName() string { return "qa_sessions" }

// QAMessage 问答消息。
type QAMessage struct {
	ID        uint64    `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	SessionID uint64    `gorm:"column:session_id;not null" json:"session_id"`
	TenantID  uint64    `gorm:"column:tenant_id;not null" json:"tenant_id"`
	UserID    uint64    `gorm:"column:user_id;not null" json:"user_id"`
	Role      string    `gorm:"column:role;size:16;not null" json:"role"`
	Content   string    `gorm:"column:content;type:text" json:"content"`
	TraceID   string    `gorm:"column:trace_id;size:128" json:"trace_id,omitempty"`
	CreatedAt time.Time `gorm:"column:created_at;type:datetime(3)" json:"created_at"`
}

func (QAMessage) TableName() string { return "qa_messages" }
