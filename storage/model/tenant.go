package model

import (
	"time"

	"gorm.io/gorm"
)

// Tenant 租户表（账号模块）
type Tenant struct {
	ID            uint64         `gorm:"column:id;primaryKey;autoIncrement"`
	Name          string         `gorm:"column:name;size:128;uniqueIndex:idx_tenants_name;not null"` // 租户名（唯一）
	Status        int8           `gorm:"column:status;default:1"`                                     // 1=启用
	QuotaLlmToken int64          `gorm:"column:quota_llm_token;default:0"`                            // LLM token 配额
	CreatedAt     time.Time      `gorm:"column:created_at;type:datetime(3)"`
	UpdatedAt     time.Time      `gorm:"column:updated_at;type:datetime(3)"`
	DeletedAt     gorm.DeletedAt `gorm:"column:deleted_at;type:datetime(3);index"`
}

func (Tenant) TableName() string { return "tenants" }

// User 用户表（账号模块）
type User struct {
	ID           uint64         `gorm:"column:id;primaryKey;autoIncrement"`
	TenantID     uint64         `gorm:"column:tenant_id;not null;uniqueIndex:idx_tenant_user"` // 参与联合唯一索引
	Username     string         `gorm:"column:username;size:64;uniqueIndex:idx_tenant_user;not null"`
	PasswordHash string         `gorm:"column:password_hash;size:256;not null"` // bcrypt
	Role         string         `gorm:"column:role;size:32;not null"`           // admin / member
	Status       int8           `gorm:"column:status;default:1"`
	CreatedAt    time.Time      `gorm:"column:created_at;type:datetime(3)"`
	UpdatedAt    time.Time      `gorm:"column:updated_at;type:datetime(3)"`
	DeletedAt    gorm.DeletedAt `gorm:"column:deleted_at;type:datetime(3);index"`
}

func (User) TableName() string { return "users" }
