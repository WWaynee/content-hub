// Package agent 定义 content-hub 多 Agent 编排的数据契约与接口。
//
// 各 agent 通过这些结构化类型交换数据（而非自由文本），保证链路可审计、可测试。
// 数据契约对应 docs/architecture/architecture.md §4。
package agent

// Evidence 知识检索 agent 输出的证据素材（一条引用来源）。
type Evidence struct {
	FileID       uint64  // 来源文档
	VersionMd5   string  // 版本
	ChunkIndex   int     // 切片序号
	ChapterTitle string  // 章节标题（可空）
	SourceText   string  // 原文原话片段（不加工）
	Score        float32 // 相似度
}

// Requirement 需求单（精简版，供 agent 使用）。
type Requirement struct {
	Title              string
	Platforms          []string
	StyleTone          string
	StyleEmotion       string
	StyleAudience      string
	StylePurpose       string
	StyleTaboo         string
	StyleSubject       string
	WordCount          int
	ChapterRequirement string
}

// Sentence 稿件中的一个句子（含证据绑定）。
type Sentence struct {
	Text         string   // 句子文本
	EvidenceRefs []uint64 // 绑定的证据（指向 Evidence 数组的索引）
}

// Paragraph 段落。
type Paragraph struct {
	Sentences []Sentence
}

// Section 章节。
type Section struct {
	Heading    string
	Paragraphs []Paragraph
}

// Article 稿件撰写 agent 输出的结构化稿件。
type Article struct {
	Title    string
	Sections []Section
}

// RetrieveRequest 知识检索 agent 的输入。
type RetrieveRequest struct {
	TenantID    uint64
	Requirement Requirement
	// 勾选范围（fileIDs 为空 = 全租户；非空 = 仅这些文档）
	FileIDs []uint64
}

// RetrieveResult 知识检索 agent 的输出。
type RetrieveResult struct {
	Evidence []Evidence
	// 检索计划（LLM 提炼出的 query 列表，供审计/排查）
	Queries []string
}

// WritingRequest 稿件撰写 agent 的输入。
type WritingRequest struct {
	Requirement Requirement
	Evidence    []Evidence
}

// EvidenceManifestEntry 证据清单中的一条。
type EvidenceManifestEntry struct {
	SentenceIndex int    // 稿件中句子序号
	SentenceText  string // 稿件句子文本
	SourceText    string // 对应资料原文片段
	FileID        uint64
	VersionMd5    string
	ChapterTitle  string
}

// EvidenceManifest 证据整理 agent 的输出。
type EvidenceManifest struct {
	Entries []EvidenceManifestEntry
}

// DialogueAction 需求对话 agent 输出的结构化操作。
type DialogueAction struct {
	// Type: update_requirement / revise_article
	Type string
	// 需求单字段更新（update_requirement 时）
	Field       string
	FieldValue  string
	// 稿件修订（revise_article 时）
	TargetSentenceIndex int
	Instruction         string
	NeedsRetrieval      bool
	RetrievalQuery      string
}
