package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/WWaynee/content-hub/agent/coordinator"
	"github.com/WWaynee/content-hub/storage"
	"github.com/WWaynee/content-hub/storage/model"
)

// ErrRunActive 同工作区已有进行中的生产任务（前端据此给可读提示）。
var ErrRunActive = storage.ErrRunActive

// 把 revision/append 接到持久 agent_run/agent_step（P05）。
// 说明：initial/regenerate(生成)的 run 化放在 handler 层(RunGeneration → agent/orchestrator 链，
// 因构建 orchestrator 需要 import retrieve/writing 等，放 service 会与这些 agent 包回环)；
// 这里只接 service 直接可达的 revise/append 两份(dispatcher 触发)。
// 并发一致性：同 ws 排他用 run.active 尽力拦截 + 稿件版本 CAS(P02) 兜底("第二 run 被 active 或 CAS 拒")。

// RunRevision 完成一次句子级修订（对话 revise_article_sentence）并记录为 revision run，产新版本。
func RunRevision(ctx context.Context, tenantID, userID, workspaceID uint64, targetIndex int, instruction string) (uint64, uint64, error) {
	baseVersion := currentArticleVersion(ctx, tenantID, workspaceID)
	co := coordinator.New()
	runRec, err := co.Start(ctx, coordinator.StartReq{
		TenantID: tenantID, UserID: userID, WorkspaceID: workspaceID,
		RunType: model.RunRevision, BaseVersion: baseVersion, CurrentRole: "writer",
	})
	if err != nil {
		if errors.Is(err, storage.ErrRunActive) {
			return 0, 0, ErrRunActive
		}
		return 0, 0, err
	}
	runID := runRec.ID
	_, _ = storage.AppendStep(ctx, runID, model.AgentStep{
		Role: model.RoleWriter, Action: "revise_sentence",
		Successor: model.RoleVerifier, Outcome: model.OutcomeAccepted,
		Decision: fmt.Sprintf("修订第 %d 句（要求：%s）基于稿件版本 %d", targetIndex, instruction, baseVersion),
	})

	verID, rerr := ReviseSentenceFull(ctx, tenantID, workspaceID, targetIndex, instruction)
	if rerr != nil {
		_ = storage.FailRun(ctx, runID, rerr.Error())
		return 0, runID, rerr
	}
	_, _ = storage.AppendStep(ctx, runID, model.AgentStep{
		Role: model.RoleWriter, Action: "revision_snapshot",
		Outcome: model.OutcomeAccepted, RefID: verID,
		Decision: fmt.Sprintf("修订完成落新版本 article_version=%d", verID),
	})
	if cerr := storage.FinishRunOk(ctx, runID, verID); cerr != nil {
		return 0, runID, cerr
	}
	return verID, runID, nil
}

// RunAppend 完成一次追加段落（对话 append_article_content）记录成 append run。
func RunAppend(ctx context.Context, tenantID, userID, workspaceID uint64, instruction string) (uint64, uint64, error) {
	baseVersion := currentArticleVersion(ctx, tenantID, workspaceID)
	co := coordinator.New()
	runRec, err := co.Start(ctx, coordinator.StartReq{
		TenantID: tenantID, UserID: userID, WorkspaceID: workspaceID,
		RunType: model.RunAppend, BaseVersion: baseVersion, CurrentRole: "writer",
	})
	if err != nil {
		if errors.Is(err, storage.ErrRunActive) {
			return 0, 0, ErrRunActive
		}
		return 0, 0, err
	}
	runID := runRec.ID
	_, _ = storage.AppendStep(ctx, runID, model.AgentStep{
		Role: model.RoleWriter, Action: "append_content",
		Successor: model.RoleVerifier, Outcome: model.OutcomeAccepted,
		Decision: fmt.Sprintf("追加段落（要求：%s）基于稿件版本 %d", instruction, baseVersion),
	})

	verID, aerr := AppendArticleContent(ctx, tenantID, workspaceID, instruction)
	if aerr != nil {
		_ = storage.FailRun(ctx, runID, aerr.Error())
		return 0, runID, aerr
	}
	_, _ = storage.AppendStep(ctx, runID, model.AgentStep{
		Role: model.RoleWriter, Action: "append_snapshot",
		Outcome: model.OutcomeAccepted, RefID: verID,
		Decision: fmt.Sprintf("追加完成落新版本 article_version=%d", verID),
	})
	if cerr := storage.FinishRunOk(ctx, runID, verID); cerr != nil {
		return 0, runID, cerr
	}
	return verID, runID, nil
}

// currentArticleVersion 返回某稿件当前版本号（无稿返回 0）。
func currentArticleVersion(ctx context.Context, tenantID, workspaceID uint64) int {
	a, err := storage.GetArticleByWorkspace(ctx, tenantID, workspaceID)
	if err != nil {
		return 0
	}
	return a.CurrentVersionNo
}
