package model

import (
	"time"

	"gorm.io/gorm"
)

// KbaseDir 知识库目录树（kbase 模块）
type KbaseDir struct {
	ID          uint64         `gorm:"column:id;primaryKey;autoIncrement"`
	TenantID    uint64         `gorm:"column:tenant_id;not null"`
	Scope       string         `gorm:"column:scope;size:16;not null"`      // public / private
	OwnerUserID uint64         `gorm:"column:owner_user_id;not null;default:0"` // private 库归属人（public 为 0）
	ParentID    uint64         `gorm:"column:parent_id;not null;default:0"`     // 父目录（0=根）
	Name        string         `gorm:"column:name;size:128;not null"`
	CreatedAt   time.Time      `gorm:"column:created_at;type:datetime(3)"`
	UpdatedAt   time.Time      `gorm:"column:updated_at;type:datetime(3)"`
	DeletedAt   gorm.DeletedAt `gorm:"column:deleted_at;type:datetime(3);index"`
}

func (KbaseDir) TableName() string { return "kbase_dirs" }

// KbaseFile 文档元数据（逻辑身份，跨版本不变）
type KbaseFile struct {
	ID                 uint64         `gorm:"column:id;primaryKey;autoIncrement"`
	TenantID           uint64         `gorm:"column:tenant_id;not null"`
	Scope              string         `gorm:"column:scope;size:16;not null"`           // public / private
	DirID              uint64         `gorm:"column:dir_id;not null"`                  // 所属目录
	OwnerUserID        uint64         `gorm:"column:owner_user_id;not null"`           // 上传者
	Name               string         `gorm:"column:name;size:256;not null"`
	CurrentVersionMd5  string         `gorm:"column:current_version_md5;size:64;not null;default:''"` // 当前最新版本 md5
	FileType           string         `gorm:"column:file_type;size:16;not null"`                        // txt / md
	Size               int64          `gorm:"column:size;default:0"`
	Active             int8           `gorm:"column:active;default:1"` // 是否可见可检索（删除=0）
	CreatedAt          time.Time      `gorm:"column:created_at;type:datetime(3)"`
	UpdatedAt          time.Time      `gorm:"column:updated_at;type:datetime(3)"`
	DeletedAt          gorm.DeletedAt `gorm:"column:deleted_at;type:datetime(3);index"`
}

func (KbaseFile) TableName() string { return "kbase_files" }

// DocVersion 文档版本（只增不减，latest 指针）
type DocVersion struct {
	ID            uint64    `gorm:"column:id;primaryKey;autoIncrement"`
	TenantID      uint64    `gorm:"column:tenant_id;not null"`
	FileID        uint64    `gorm:"column:file_id;not null;uniqueIndex:idx_file_version"`
	VersionMd5    string    `gorm:"column:version_md5;size:64;not null;uniqueIndex:idx_file_version"`
	VersionNo     int       `gorm:"column:version_no;not null;default:1"`
	OSSObjectKey  string    `gorm:"column:oss_object_key;size:512;not null"`
	Latest        int8      `gorm:"column:latest;default:1"`
	Status        string    `gorm:"column:status;size:32;not null;default:'pending'"` // pending/processing/success/fail
	ErrorMsg      string    `gorm:"column:error_msg;type:text"`
	UploaderUserID uint64   `gorm:"column:uploader_user_id;not null"`
	CreatedAt     time.Time `gorm:"column:created_at;type:datetime(3)"`
	UpdatedAt     time.Time `gorm:"column:updated_at;type:datetime(3)"`
}

func (DocVersion) TableName() string { return "doc_versions" }

// DocChunk 文档切片（原文，去重）
type DocChunk struct {
	ID          uint64    `gorm:"column:id;primaryKey;autoIncrement"`
	TenantID    uint64    `gorm:"column:tenant_id;not null"`
	FileID      uint64    `gorm:"column:file_id;not null;uniqueIndex:idx_file_version_chunk"`
	VersionMd5  string    `gorm:"column:version_md5;size:64;not null;uniqueIndex:idx_file_version_chunk"`
	ChunkIndex  int       `gorm:"column:chunk_index;not null;uniqueIndex:idx_file_version_chunk"`
	ChapterTitle string   `gorm:"column:chapter_title;size:256"` // 提取不到留空
	Content     string    `gorm:"column:content;type:longtext"`  // 切片原文（~300 字，完整句末截断）
	StartChar   int       `gorm:"column:start_char;default:0"`
	EndChar     int       `gorm:"column:end_char;default:0"`
	CreatedAt   time.Time `gorm:"column:created_at;type:datetime(3)"`
}

func (DocChunk) TableName() string { return "doc_chunks" }

// DocSentence 文档句（原文，去重）
type DocSentence struct {
	ID            uint64    `gorm:"column:id;primaryKey;autoIncrement"`
	TenantID      uint64    `gorm:"column:tenant_id;not null"`
	FileID        uint64    `gorm:"column:file_id;not null"`
	VersionMd5    string    `gorm:"column:version_md5;size:64;not null"`
	ChunkID       uint64    `gorm:"column:chunk_id;not null"` // 所属切片
	SentenceIndex int       `gorm:"column:sentence_index;not null"`
	Content       string    `gorm:"column:content;type:text"`
	StartChar     int       `gorm:"column:start_char;default:0"`
	EndChar       int       `gorm:"column:end_char;default:0"`
	CreatedAt     time.Time `gorm:"column:created_at;type:datetime(3)"`
}

func (DocSentence) TableName() string { return "doc_sentences" }
