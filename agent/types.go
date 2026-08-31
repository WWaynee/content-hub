// Package agent 定义 content-hub 多 Agent 编排的数据契约与接口。
//
// 各 agent 通过这些结构化类型交换数据（而非自由文本），保证链路可审计、可测试。
// 数据契约对应 docs/architecture/architecture.md §4。
package agent

// Evidence 知识检索 agent 输出的证据素材（一次检索命中的「句子级」证据，方案一）。
type Evidence struct {
	FileID        uint64  // 来源文档
	DocSentenceID uint64  // 来源文档句 ID（句子级锚点）
	ChunkID       uint64  // 所属切片 ID
	VersionMd5    string  // 版本
	ChunkIndex    int     // 切片序号
	ChapterTitle  string  // 章节标题（可空）
	SourceText    string  // 原文原话片段（句子级，不加工）
	Score         float32 // 相似度
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

// DialogueAction 对话 agent 输出的单个原子动作（工具调用）。
type DialogueAction struct {
	// Tool 动作类型（受白名单约束）：
	//   update_requirement_field / request_retrieval / append_article_content / revise_article_sentence
	Tool string `json:"tool"`
	// 需求单字段更新（update_requirement_field 时）
	Field      string `json:"field,omitempty"`
	FieldValue string `json:"field_value,omitempty"`
	// 修订稿件（revise/append 时）
	TargetSentenceIndex int    `json:"target_sentence_index,omitempty"`
	Instruction         string `json:"instruction,omitempty"`
	Position            string `json:"position,omitempty"` // append_article_content 的位置（如 last）
	// 请求补检索（request_retrieval 时）
	NeedsRetrieval bool   `json:"needs_retrieval,omitempty"`
	RetrievalQuery string `json:"retrieval_query,omitempty"`
}

// DialoguePlan 对话 agent 输出的动作计划（一个对话可含多个动作）。
type DialoguePlan struct {
	Actions []DialogueAction `json:"actions"`
}
