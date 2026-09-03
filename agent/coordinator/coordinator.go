// Package coordinator 提供稿件 agent_run/agent_step 的编排原语。
//
// P05 语义（RFC rev-1 §2.1）：把一次稿件生产过程固化为持久 run + 可回放 step，
// 以响应评审"你是 workflow / 中间不可审 / 不可回放"的质疑（A2：持久副作用）。
// Coordinator 只承载 run 生命周期的确定性推进 + 派发打点；LLM 决策(GUARDIAN/Retriever
// 真多轮与 ask_human)在 P06 才作为 agent 决策接入，此处已为其留 successor 落点。
package coordinator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/WWaynee/content-hub/storage"
	"github.com/WWaynee/content-hub/storage/model"
)

// 对外错误
var ErrRunActive = storage.ErrRunActive

// ValidTransition：run.status 合法迁移表（状态机单测据此断言）。
var ValidTransition = map[string]map[string]bool{
	string(model.RunRunning): {
		string(model.RunSuccess):       true,
		string(model.RunFailed):        true,
		string(model.RunAwaitingHuman): true,
		string(model.RunCancelled):     true,
	},
	string(model.RunAwaitingHuman): {
		string(model.RunRunning):   true,
		string(model.RunCancelled): true,
		string(model.RunFailed):    true,
	},
	string(model.RunCancelled): {},
	string(model.RunSuccess):   {},
	string(model.RunFailed):    {},
}

// CanTransition 判定 from→to 状态迁移是否合法。
func CanTransition(from, to model.RunStatus) bool {
	return ValidTransition[string(from)][string(to)]
}

// StartReq 开启 run 的入参。
type StartReq struct {
	TenantID     uint64
	UserID       uint64
	WorkspaceID  uint64
	RunType      model.RunType
	BaseVersion  int      // 基于稿件版本（0=首篇首次）
	Plan         any      // 可持久化的 plan（Claim 等），无则 nil
	CurrentRole  string   // 首个打点角色（如 planner）
}

// Coordinator run 生命周期编排。
type Coordinator struct{}

// New 构造。
func New() *Coordinator { return &Coordinator{} }

// Start 开启新 run。
func (c *Coordinator) Start(ctx context.Context, req StartReq) (*model.AgentRun, error) {
	if req.WorkspaceID == 0 || req.TenantID == 0 || req.UserID == 0 {
		return nil, errors.New("run 需要 tenant/user/workspace 上下文")
	}
	var planText string
	if req.Plan != nil {
		b, err := json.Marshal(req.Plan)
		if err != nil {
			return nil, fmt.Errorf("序列化 run.plan 失败: %w", err)
		}
		planText = string(b)
	}
	runID, err := storage.BeginRun(ctx, req.TenantID, req.UserID, req.WorkspaceID,
		string(req.RunType), req.BaseVersion, planText, req.CurrentRole)
	if err != nil {
		return nil, err
	}
	return storage.GetRun(ctx, req.TenantID, runID.ID)
}

// NoteStep 记录一步并推进 current_role/action。
func (c *Coordinator) NoteStep(ctx context.Context, runID uint64, s model.AgentStep) error {
	if _, err := storage.AppendStep(ctx, runID, s); err != nil {
		return err
	}
	return storage.UpdateRunProgress(ctx, runID, s.Role, s.Action)
}

// Success 完成 run 并产出版本（status→success）。
func (c *Coordinator) Success(ctx context.Context, tenantID, runID, resultVersionID uint64) error {
	r, err := storage.GetRun(ctx, tenantID, runID)
	if err != nil {
		return err
	}
	if !CanTransition(model.RunStatus(r.Status), model.RunSuccess) {
		return fmt.Errorf("非法 run 状态迁移 %s→success", r.Status)
	}
	return storage.FinishRunOk(ctx, runID, resultVersionID)
}

// MarkAwaitingHuman 置 run 等待人工并打一个 ask_human step（P06 guardian ask_human 时停等）。
func (c *Coordinator) MarkAwaitingHuman(ctx context.Context, runID uint64, reason string) error {
	_, _ = storage.AppendStep(ctx, runID, model.AgentStep{
		Role:      model.RoleMatchHuman,
		Action:    "ask_human",
		Successor: "guardian",
		Outcome:   model.OutcomeAwaitHuman,
		Decision:  reason,
	})
	return storage.MarkAwaitingHuman(ctx, runID, model.RoleMatchHuman, "asking")
}

// Fail run 失败释放 active，reason 供 UI 显示卡点。
func (c *Coordinator) Fail(ctx context.Context, runID uint64, reason string) error {
	return storage.FailRun(ctx, runID, reason)
}

// Cancel run（幂等停止）。
func (c *Coordinator) Cancel(ctx context.Context, runID uint64) error {
	return storage.CancelRun(ctx, runID)
}
