package model

import "time"

// RetrievalBatch 检索快照：一次检索的产物（只在标识符层记录命中，不冗余文本）。
type RetrievalBatch struct {
	ID                 uint64    `gorm:"column:id;primaryKey;autoIncrement"`
	TenantID           uint64    `gorm:"column:tenant_id;not null"`
	WorkspaceID        uint64    `gorm:"column:workspace_id;not null"`
	RequirementID      uint64    `gorm:"column:requirement_id;not null"`
	RequirementVersion int       `gorm:"column:requirement_version;not null"` // 该批次基于的需求单版本
	Queries            string    `gorm:"column:queries;type:text"`            // 检索 query 列表（JSON 数组字符串，审计用）
	CreatedAt          time.Time `gorm:"column:created_at;type:datetime(3)"`
}

func (RetrievalBatch) TableName() string { return "retrieval_batches" }

// RetrievalBatchItem 检索批次内命中的文档句指针。
// 只存标识符，不存文本（文本由 doc_sentences 表按 id 求取）。
type RetrievalBatchItem struct {
	ID            uint64    `gorm:"column:id;primaryKey;autoIncrement"`
	BatchID       uint64    `gorm:"column:batch_id;not null;uniqueIndex:idx_batch_sentence"`
	TenantID      uint64    `gorm:"column:tenant_id;not null"`
	DocSentenceID uint64    `gorm:"column:doc_sentence_id;not null;uniqueIndex:idx_batch_sentence"`
	DocFileID     uint64    `gorm:"column:doc_file_id;not null"`
	VersionMd5    string    `gorm:"column:version_md5;size:64;not null"`
	ChunkID       uint64    `gorm:"column:chunk_id;not null"`
	ChapterTitle  string    `gorm:"column:chapter_title;size:256"`
	Score         float64   `gorm:"column:score"`
	CreatedAt     time.Time `gorm:"column:created_at;type:datetime(3)"`
}

func (RetrievalBatchItem) TableName() string { return "retrieval_batch_items" }
