package model

import (
	"time"

	"gorm.io/gorm"
)

// Article 稿件（快照式，指向当前版本）
type Article struct {
	ID               uint64         `gorm:"column:id;primaryKey;autoIncrement"`
	WorkspaceID      uint64         `gorm:"column:workspace_id;not null"`
	TenantID         uint64         `gorm:"column:tenant_id;not null"`
	CurrentVersionNo int            `gorm:"column:current_version_no;not null;default:0"` // 当前稿件版本号
	Title            string         `gorm:"column:title;size:256;not null;default:''"`
	Status           string         `gorm:"column:status;size:32;not null;default:'none'"` // none/generated/revising/failed
	CreatedAt        time.Time      `gorm:"column:created_at;type:datetime(3)"`
	UpdatedAt        time.Time      `gorm:"column:updated_at;type:datetime(3)"`
	DeletedAt        gorm.DeletedAt `gorm:"column:deleted_at;type:datetime(3);index"`
}

func (Article) TableName() string { return "articles" }

// ArticleVersion 稿件版本（快照，完成态）
type ArticleVersion struct {
	ID                uint64    `gorm:"column:id;primaryKey;autoIncrement"`
	ArticleID         uint64    `gorm:"column:article_id;not null;uniqueIndex:idx_article_version"`
	WorkspaceID       uint64    `gorm:"column:workspace_id;not null"`
	TenantID          uint64    `gorm:"column:tenant_id;not null"`
	VersionNo         int       `gorm:"column:version_no;not null;uniqueIndex:idx_article_version"`
	FullContent       string    `gorm:"column:full_content;type:longtext"` // 整篇 markdown
	Status            string    `gorm:"column:status;size:32;not null;default:'completed'"`
	ReferencedVersion int       `gorm:"column:referenced_version;not null;default:0"`
	CreatedAt         time.Time `gorm:"column:created_at;type:datetime(3)"`
}

func (ArticleVersion) TableName() string { return "article_versions" }

// ArticleSentence 稿件句（稿件本体）
type ArticleSentence struct {
	ID               uint64    `gorm:"column:id;primaryKey;autoIncrement"`
	ArticleVersionID uint64    `gorm:"column:article_version_id;not null"`
	WorkspaceID      uint64    `gorm:"column:workspace_id;not null"`
	TenantID         uint64    `gorm:"column:tenant_id;not null"`
	SectionIndex     int       `gorm:"column:section_index;not null;default:0"`
	ParagraphIndex   int       `gorm:"column:paragraph_index;not null;default:0"`
	SentenceIndex    int       `gorm:"column:sentence_index;not null;default:0"`
	Content          string    `gorm:"column:content;type:text"`
	CreatedAt        time.Time `gorm:"column:created_at;type:datetime(3)"`
}

func (ArticleSentence) TableName() string { return "article_sentences" }

// EvidenceBinding 证据绑定（稿件句 ↔ 文档句）
type EvidenceBinding struct {
	ID                uint64    `gorm:"column:id;primaryKey;autoIncrement"`
	ArticleVersionID  uint64    `gorm:"column:article_version_id;not null"`
	ArticleSentenceID uint64    `gorm:"column:article_sentence_id;not null;uniqueIndex:idx_evidence"`
	TenantID          uint64    `gorm:"column:tenant_id;not null"`
	SourceType        string    `gorm:"column:source_type;size:32;not null;default:'knowledge'"` // knowledge / none
	DocFileID         uint64    `gorm:"column:doc_file_id;not null;default:0"`
	DocSentenceID     uint64    `gorm:"column:doc_sentence_id;not null;default:0;uniqueIndex:idx_evidence"`
	EvidenceStatus    string    `gorm:"column:evidence_status;size:32;not null;default:'bound'"` // bound/no_source
	OrderNo           int       `gorm:"column:order_no;not null;default:0"`
	CreatedAt         time.Time `gorm:"column:created_at;type:datetime(3)"`
}

func (EvidenceBinding) TableName() string { return "evidence_bindings" }
