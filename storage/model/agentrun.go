package model

import "time"

// P05 / RFC rev-1 §2.1：把"一次稿件生产过程"固化为持久会话 agent_run + agent_step。
// 解决评审"你是 workflow"：每一步派给谁、谁决定下一步、卡在哪，都落库可回放、可审计(A2)。

// RunType run 的种类
type RunType string

const (
	RunInitial     RunType = "initial"      // 从需求单起稿
	RunRevision    RunType = "revision"     // 改一句
	RunAppend      RunType = "append"       // 追加一段
	RunRegenerate  RunType = "regenerate"   // 全文重生成
	RunSequence    RunType = "sequence"     // P08：一次 change_list(edit/insert/delete/move)为受控编辑落地为新版本
	RunDraftAssist RunType = "draft_assist" // P10：拿用户粘贴的草稿起稿（draft_input → split → 治理 → 首版）
)

// RunStatus run 生命周期状态
type RunStatus string

const (
	RunRunning       RunStatus = "running"        // 进行中（LLM/编排推进中）
	RunAwaitingHuman RunStatus = "awaiting_human" // 已停下来等人补料/确认（P06 ask_human）
	RunSuccess       RunStatus = "success"        // 完成并已产新版本
	RunFailed        RunStatus = "failed"         // 失败（保留原因/停点）
	RunCancelled     RunStatus = "cancelled"      // 用户/幂等取消
)

// 步骤角色（RFC §2.2：planning/retrieval/guardian/writer/verifier/match_human 编排时派给谁）。
// 以 string 常量表达，避免 agent_step.role 引入额外枚举复杂度。
const (
	RolePlanner    = "planner"     // 拆需求→Claim
	RoleRetriever  = "retriever"   // 检索证
	RoleGuardian   = "guardian"    // 裁决 accept/retry/ask_human
	RoleWriter     = "writer"      // 撰写/改写
	RoleVerifier   = "verifier"    // 规则/事实校验
	RoleEvidence   = "evidence"    // 证据整理
	RoleMatchHuman = "match_human" // 对话/等待人工
)

// StepOutcome 单步分级结论。
const (
	OutcomeAccepted   = "accepted"    // 通过
	OutcomeRejected   = "rejected"    // 打回
	OutcomeRaisedFlag = "raised_flag" // 发起需人工复核
	OutcomeAwaitHuman = "await_human" // 等待人工
)

// AgentRun 一篇稿件的一次生产会话（一等持久实体）。
// active 字段语义：某 workspace 同一时刻仅允许 1 条 non-terminal(running/awaiting_human) run 为 active。
// 并发一致性另由稿件版本 CAS(P02) 兜底（本表用于状态/审计/await_human/P06 Guardian 接力）。
type AgentRun struct {
	ID          uint64 `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	TenantID    uint64 `gorm:"column:tenant_id;not null" json:"tenant_id"`
	UserID      uint64 `gorm:"column:user_id;not null;default:0" json:"user_id"`
	WorkspaceID uint64 `gorm:"column:workspace_id;not null;index:idx_ws_active" json:"workspace_id"`
	RunType     string `gorm:"column:run_type;size:32;not null" json:"run_type"`
	// BaseArticleVersion 该 run 基于的稿件版本号（0=首次尚未有版本）。
	BaseArticleVersion int `gorm:"column:base_article_version;not null;default:0" json:"base_article_version"`
	// ResultVersionID 结束后产出的 article_version.id（失败/取消时为 0）。
	ResultVersionID uint64 `gorm:"column:result_version_id;not null;default:0" json:"result_version_id"`
	Status          string `gorm:"column:status;size:32;not null;default:'running'"`
	Active          bool   `gorm:"column:active;not null;default:true"` // 同 ws 排他，见 struct 注释
	// Plan JSON（Planner 拆出的 Claim[]，initial 后填；revision/append 可直接空）。
	Plan string `gorm:"column:plan;type:json" json:"plan,omitempty"`
	// CurrentRole/CurrentAction 供 UI 人话"现在跑到哪一步/派给谁"。
	CurrentRole   string `gorm:"column:current_role;size:32" json:"current_role,omitempty"`
	CurrentAction string `gorm:"column:current_action;size:64" json:"current_action,omitempty"`
	// ErrorMsg 失败原因（status=failed）
	ErrorMsg  string    `gorm:"column:error_msg;type:text" json:"error_msg,omitempty"`
	CreatedAt time.Time `gorm:"column:created_at;type:datetime(3)" json:"created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at;type:datetime(3)" json:"updated_at"`
}

func (AgentRun) TableName() string { return "agent_runs" }

// AgentStep run 内一步的执行记录（可回放：这步是谁、做了什么、决定下一步派给谁）。
type AgentStep struct {
	ID          uint64 `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	RunID       uint64 `gorm:"column:run_id;not null;uniqueIndex:idx_run_step" json:"run_id"`
	TenantID    uint64 `gorm:"column:tenant_id;not null" json:"tenant_id"`
	WorkspaceID uint64 `gorm:"column:workspace_id;not null" json:"workspace_id"`
	StepNo      int    `gorm:"column:step_no;not null;uniqueIndex:idx_run_step" json:"step_no"`
	// Role 决定执行者：planner|retriever|guardian|writer|verifier|match_human(全部 P05 先各打点，P06 再动真循环)
	Role string `gorm:"column:role;size:32;not null" json:"role"`
	// Action 本步具体动作：如 search/write/verify/decision。
	Action string `gorm:"column:action;size:64;not null" json:"action"`
	// Decision 本步的决策说明（人话/结构化摘要，不存大原文，正文取正式产物）
	Decision string `gorm:"column:decision;type:text" json:"decision,omitempty"`
	// Successor 决定下一步派给谁（Guardian 用；本 agent 自接时填自身）
	Successor string `gorm:"column:successor;size:32" json:"successor,omitempty"`
	// Outcome 分级结论
	Outcome string `gorm:"column:outcome;size:32" json:"outcome,omitempty"`
	// RefID 关联的 article_version 等正式产物 id（可选）
	RefID     uint64    `gorm:"column:ref_id;not null;default:0" json:"ref_id,omitempty"`
	CreatedAt time.Time `gorm:"column:created_at;type:datetime(3)" json:"created_at"`
}

func (AgentStep) TableName() string { return "agent_steps" }
