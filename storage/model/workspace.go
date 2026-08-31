package model

import (
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// Workspace 工作区（稿件工作流容器，owner 专属）
type Workspace struct {
	ID          uint64         `gorm:"column:id;primaryKey;autoIncrement"`
	TenantID    uint64         `gorm:"column:tenant_id;not null"`
	OwnerUserID uint64         `gorm:"column:owner_user_id;not null"`
	Title       string         `gorm:"column:title;size:256;not null;default:''"`
	Status      string         `gorm:"column:status;size:32;not null;default:'draft'"` // draft/needs_req/generating/generated/revising/failed
	CreatedAt   time.Time      `gorm:"column:created_at;type:datetime(3)"`
	UpdatedAt   time.Time      `gorm:"column:updated_at;type:datetime(3);index:idx_ws_updated_at,sort:desc"`
	DeletedAt   gorm.DeletedAt `gorm:"column:deleted_at;type:datetime(3);index"`
}

func (Workspace) TableName() string { return "workspaces" }

// Requirement 需求单（workspace 一对一）
type Requirement struct {
	ID                uint64         `gorm:"column:id;primaryKey;autoIncrement"`
	WorkspaceID       uint64         `gorm:"column:workspace_id;not null"`
	TenantID          uint64         `gorm:"column:tenant_id;not null"`
	Title             string         `gorm:"column:title;size:256;not null"`
	Tags              datatypes.JSON `gorm:"column:tags;type:json"`      // 标签数组
	Platforms         datatypes.JSON `gorm:"column:platforms;type:json"` // 发布平台枚举数组
	StyleTone         string         `gorm:"column:style_tone;size:255"`
	StyleEmotion      string         `gorm:"column:style_emotion;size:255"`
	StyleAudience     string         `gorm:"column:style_audience;size:255"`
	StylePurpose      string         `gorm:"column:style_purpose;size:255"`
	StyleTaboo        string         `gorm:"column:style_taboo;type:text"`
	StyleSubject      string         `gorm:"column:style_subject;size:255"`
	WordCount         int            `gorm:"column:word_count;default:0"`
	ChapterRequirement string        `gorm:"column:chapter_requirement;type:text"`
	Version           int            `gorm:"column:version;not null;default:1"` // 需求单版本号，每次变更 +1（驱动惰性失效）
	CreatedAt         time.Time      `gorm:"column:created_at;type:datetime(3)"`
	UpdatedAt         time.Time      `gorm:"column:updated_at;type:datetime(3)"`
}

func (Requirement) TableName() string { return "requirements" }

// RequirementScope 需求单引用范围（活引用，跟随目录）
type RequirementScope struct {
	ID            uint64    `gorm:"column:id;primaryKey;autoIncrement"`
	RequirementID uint64    `gorm:"column:requirement_id;not null;uniqueIndex:idx_req_scope"`
	TenantID      uint64    `gorm:"column:tenant_id;not null"`
	ScopeType     string    `gorm:"column:scope_type;size:16;not null;uniqueIndex:idx_req_scope"` // public/private
	TargetType    string    `gorm:"column:target_type;size:16;not null;uniqueIndex:idx_req_scope"` // dir/file
	DirID         uint64    `gorm:"column:dir_id;not null;default:0;uniqueIndex:idx_req_scope"`
	FileID        uint64    `gorm:"column:file_id;not null;default:0;uniqueIndex:idx_req_scope"`
	CreatedAt     time.Time `gorm:"column:created_at;type:datetime(3)"`
}

func (RequirementScope) TableName() string { return "requirement_scope" }
