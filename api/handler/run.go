package handler

import (
	"context"

	"github.com/WWaynee/content-hub/agent/coordinator"
	"github.com/WWaynee/content-hub/storage"
	"github.com/WWaynee/content-hub/storage/model"
)

// run.go — P05 稿件生成/修订的 agent_run 接入（handler 层编排）。
// 说明：initial(全文)run 的编排在 handler 完成，因为构建 orchestrator 需要 import
// retrieve/writing 等 agent 包（这些包反向引用 service），放 service 会造成 import 回环。

// beginInitialRun 为一次 initial/regenerate 生成创建并返回 run（active 排他检查在 storage.BeginRun）。
// 基于稿件当前版本号（首次无稿=0）。写 planner 起始 step。并发重发时返回 storage.ErrRunActive。
func beginInitialRun(ctx context.Context, tenantID, userID, workspaceID uint64) (*model.AgentRun, error) {
	base := 0
	if a, err := storage.GetArticleByWorkspace(ctx, tenantID, workspaceID); err == nil {
		base = a.CurrentVersionNo
	}
	co := coordinator.New()
	rec, err := co.Start(ctx, coordinator.StartReq{
		TenantID: tenantID, UserID: userID, WorkspaceID: workspaceID,
		RunType: model.RunInitial, BaseVersion: base, CurrentRole: "planner",
	})
	if err != nil {
		return nil, err
	}
	_, _ = storage.AppendStep(ctx, rec.ID, model.AgentStep{
		Role: model.RolePlanner, Action:   "start_generation",
		Successor: model.RoleRetriever, Outcome: model.OutcomeAccepted,
		Decision: "开始稿件生成（initial），检索并撰写",
	})
	return rec, nil
}
