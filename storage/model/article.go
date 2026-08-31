package model

import (
	"time"

	"gorm.io/gorm"
)

// Article 稿件（快照式，指向当前版本）
type Article struct {
	ID               uint64         `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	WorkspaceID      uint64         `gorm:"column:workspace_id;not null" json:"workspace_id"`
	TenantID         uint64         `gorm:"column:tenant_id;not null" json:"tenant_id"`
	CurrentVersionNo int            `gorm:"column:current_version_no;not null;default:0" json:"current_version_no"`
	Title            string         `gorm:"column:title;size:256;not null;default:''" json:"title"`
	Status           string         `gorm:"column:status;size:32;not null;default:'none'" json:"status"`
	CreatedAt        time.Time      `gorm:"column:created_at;type:datetime(3)" json:"created_at"`
	UpdatedAt        time.Time      `gorm:"column:updated_at;type:datetime(3)" json:"updated_at"`
	DeletedAt        gorm.DeletedAt `gorm:"column:deleted_at;type:datetime(3);index" json:"deleted_at,omitempty"`
}

func (Article) TableName() string { return "articles" }

// ArticleVersion 稿件版本（快照，完成态）
type ArticleVersion struct {
	ID                uint64    `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	ArticleID         uint64    `gorm:"column:article_id;not null;uniqueIndex:idx_article_version" json:"article_id"`
	WorkspaceID       uint64    `gorm:"column:workspace_id;not null" json:"workspace_id"`
	TenantID          uint64    `gorm:"column:tenant_id;not null" json:"tenant_id"`
	VersionNo         int       `gorm:"column:version_no;not null;uniqueIndex:idx_article_version" json:"version_no"`
	FullContent       string    `gorm:"column:full_content;type:longtext" json:"full_content"`
	Status            string    `gorm:"column:status;size:32;not null;default:'completed'" json:"status"`
	ReferencedVersion int       `gorm:"column:referenced_version;not null;default:0" json:"referenced_version"`
	CreatedAt         time.Time `gorm:"column:created_at;type:datetime(3)" json:"created_at"`
}

func (ArticleVersion) TableName() string { return "article_versions" }

// ArticleSentence 稿件句（稿件本体）
type ArticleSentence struct {
	ID               uint64    `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	ArticleVersionID uint64    `gorm:"column:article_version_id;not null" json:"article_version_id"`
	WorkspaceID      uint64    `gorm:"column:workspace_id;not null" json:"workspace_id"`
	TenantID         uint64    `gorm:"column:tenant_id;not null" json:"tenant_id"`
	SectionIndex     int       `gorm:"column:section_index;not null;default:0" json:"section_index"`
	ParagraphIndex   int       `gorm:"column:paragraph_index;not null;default:0" json:"paragraph_index"`
	SentenceIndex    int       `gorm:"column:sentence_index;not null;default:0" json:"sentence_index"`
	Content          string    `gorm:"column:content;type:text" json:"content"`
	CreatedAt        time.Time `gorm:"column:created_at;type:datetime(3)" json:"created_at"`
}

func (ArticleSentence) TableName() string { return "article_sentences" }

// EvidenceBinding 证据绑定（稿件句 ↔ 文档句）
type EvidenceBinding struct {
	ID                uint64    `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	ArticleVersionID  uint64    `gorm:"column:article_version_id;not null" json:"article_version_id"`
	ArticleSentenceID uint64    `gorm:"column:article_sentence_id;not null;uniqueIndex:idx_evidence" json:"article_sentence_id"`
	TenantID          uint64    `gorm:"column:tenant_id;not null" json:"tenant_id"`
	SourceType        string    `gorm:"column:source_type;size:32;not null;default:'knowledge'" json:"source_type"`
	DocFileID         uint64    `gorm:"column:doc_file_id;not null;default:0" json:"doc_file_id"`
	DocSentenceID     uint64    `gorm:"column:doc_sentence_id;not null;default:0;uniqueIndex:idx_evidence" json:"doc_sentence_id"`
	EvidenceStatus    string    `gorm:"column:evidence_status;size:32;not null;default:'bound'" json:"evidence_status"`
	OrderNo           int       `gorm:"column:order_no;not null;default:0" json:"order_no"`
	CreatedAt         time.Time `gorm:"column:created_at;type:datetime(3)" json:"created_at"`
}

func (EvidenceBinding) TableName() string { return "evidence_bindings" }
