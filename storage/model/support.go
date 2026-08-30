package model

import (
	"time"
)

// AgentTask 异步任务（文档解析 / 稿件生成）
type AgentTask struct {
	ID       uint64    `gorm:"column:id;primaryKey;autoIncrement"`
	TenantID uint64    `gorm:"column:tenant_id;not null"`
	TaskType string    `gorm:"column:task_type;size:64;not null"` // document_parse / article_generate
	BizID    uint64    `gorm:"column:biz_id;not null;default:0"`
	Status   string    `gorm:"column:status;size:32;not null;default:'pending'"` // pending/processing/success/failed
	ErrorMsg string    `gorm:"column:error_msg;type:text"`
	RetryCount int    `gorm:"column:retry_count;not null;default:0"`
	CreatedAt time.Time `gorm:"column:created_at;type:datetime(3)"`
	UpdatedAt time.Time `gorm:"column:updated_at;type:datetime(3)"`
}

func (AgentTask) TableName() string { return "agent_tasks" }

// AuditLog 审计日志
type AuditLog struct {
	ID        uint64    `gorm:"column:id;primaryKey;autoIncrement"`
	TenantID  uint64    `gorm:"column:tenant_id;not null"`
	UserID    uint64    `gorm:"column:user_id;not null;default:0"`
	Operation string    `gorm:"column:operation;size:128;not null"`
	TraceID   string    `gorm:"column:trace_id;size:128"`
	Content   string    `gorm:"column:content;type:text"`
	CreatedAt time.Time `gorm:"column:created_at;type:datetime(3)"`
}

func (AuditLog) TableName() string { return "audit_logs" }
