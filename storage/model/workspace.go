package model

import (
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// Workspace 工作区（稿件工作流容器，owner 专属）
type Workspace struct {
	ID          uint64         `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	TenantID    uint64         `gorm:"column:tenant_id;not null" json:"tenant_id"`
	OwnerUserID uint64         `gorm:"column:owner_user_id;not null" json:"owner_user_id"`
	Title       string         `gorm:"column:title;size:256;not null;default:''" json:"title"`
	Status      string         `gorm:"column:status;size:32;not null;default:'draft'" json:"status"`
	CreatedAt   time.Time      `gorm:"column:created_at;type:datetime(3)" json:"created_at"`
	UpdatedAt   time.Time      `gorm:"column:updated_at;type:datetime(3);index:idx_ws_updated_at,sort:desc" json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"column:deleted_at;type:datetime(3);index" json:"deleted_at,omitempty"`
}

func (Workspace) TableName() string { return "workspaces" }

// Requirement 需求单（workspace 一对一）
type Requirement struct {
	ID                 uint64         `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	WorkspaceID        uint64         `gorm:"column:workspace_id;not null" json:"workspace_id"`
	TenantID           uint64         `gorm:"column:tenant_id;not null" json:"tenant_id"`
	Title              string         `gorm:"column:title;size:256;not null" json:"title"`
	Tags               datatypes.JSON `gorm:"column:tags;type:json" json:"tags"`
	Platforms          datatypes.JSON `gorm:"column:platforms;type:json" json:"platforms"`
	StyleTone          string         `gorm:"column:style_tone;size:255" json:"style_tone"`
	StyleEmotion       string         `gorm:"column:style_emotion;size:255" json:"style_emotion"`
	StyleAudience      string         `gorm:"column:style_audience;size:255" json:"style_audience"`
	StylePurpose       string         `gorm:"column:style_purpose;size:255" json:"style_purpose"`
	StyleTaboo         string         `gorm:"column:style_taboo;type:text" json:"style_taboo"`
	StyleSubject       string         `gorm:"column:style_subject;size:255" json:"style_subject"`
	WordCount          int            `gorm:"column:word_count;default:0" json:"word_count"`
	ChapterRequirement string         `gorm:"column:chapter_requirement;type:text" json:"chapter_requirement"`
	Version            int            `gorm:"column:version;not null;default:1" json:"version"`
	CreatedAt          time.Time      `gorm:"column:created_at;type:datetime(3)" json:"created_at"`
	UpdatedAt          time.Time      `gorm:"column:updated_at;type:datetime(3)" json:"updated_at"`
}

func (Requirement) TableName() string { return "requirements" }

// RequirementScope 需求单引用范围（活引用，跟随目录）
type RequirementScope struct {
	ID            uint64    `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	RequirementID uint64    `gorm:"column:requirement_id;not null;uniqueIndex:idx_req_scope" json:"requirement_id"`
	TenantID      uint64    `gorm:"column:tenant_id;not null" json:"tenant_id"`
	ScopeType     string    `gorm:"column:scope_type;size:16;not null;uniqueIndex:idx_req_scope" json:"scope_type"`
	TargetType    string    `gorm:"column:target_type;size:16;not null;uniqueIndex:idx_req_scope" json:"target_type"`
	DirID         uint64    `gorm:"column:dir_id;not null;default:0;uniqueIndex:idx_req_scope" json:"dir_id"`
	FileID        uint64    `gorm:"column:file_id;not null;default:0;uniqueIndex:idx_req_scope" json:"file_id"`
	CreatedAt     time.Time `gorm:"column:created_at;type:datetime(3)" json:"created_at"`
}

func (RequirementScope) TableName() string { return "requirement_scope" }
