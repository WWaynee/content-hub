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

// RunSequenceEdit 完成一次受控序列编辑，并把其记录成 sequence run（P08）。
// - 先创建 run(active 排他)、打 edit step；
// - 调用 applySequenceVersion(CAS+事务) 产新版本；
// - 成功则 finish(${version}) / 失败则 FailRun(reason)。
// 返回 (newVersionID, 顺手带人话 reviews, err)。
func RunSequenceEdit(ctx context.Context, tenantID, userID, workspaceID uint64, req *ChangeListRequest) (uint64, []string, error) {
	baseVersion := currentArticleVersion(ctx, tenantID, workspaceID)
	co := coordinator.New()
	runRec, err := co.Start(ctx, coordinator.StartReq{
		TenantID: tenantID, UserID: userID, WorkspaceID: workspaceID,
		RunType: model.RunSequence, BaseVersion: baseVersion, CurrentRole: "writer", Plan: req,
	})
	if err != nil {
		if errors.Is(err, storage.ErrRunActive) {
			return 0, nil, ErrRunActive
		}
		return 0, nil, err
	}
	runID := runRec.ID
	opSummary := make([]string, 0, len(req.Ops))
	for _, op := range req.Ops {
		opSummary = append(opSummary, op.Op)
	}
	_, _ = storage.AppendStep(ctx, runID, model.AgentStep{
		Role: model.RoleWriter, Action: "apply_change_list",
		Successor: model.RoleVerifier, Outcome: model.OutcomeAccepted,
		Decision: fmt.Sprintf("对稿件执行手编序列编辑(%v)，基于稿件版本 %d", opSummary, baseVersion),
	})

	verID, reviews, aerr := applySequenceVersion(ctx, tenantID, workspaceID, req)
	if aerr != nil {
		_ = storage.FailRun(ctx, runID, aerr.Error())
		return 0, reviews, aerr
	}
	if len(reviews) > 0 {
		// 有 no_source 等待核项：作为一个"待复核" step 留在 run 里
		_, _ = storage.AppendStep(ctx, runID, model.AgentStep{
			Role: model.RoleMatchHuman, Action: "no_source_flag",
			Outcome:  model.OutcomeAwaitHuman,
			Decision: "本次序列编辑中有句子未获外部来源，正文已保留；是否补资料或放弃该句交由作者决定(P09)",
		})
	}
	_, _ = storage.AppendStep(ctx, runID, model.AgentStep{
		Role: model.RoleWriter, Action: "sequence_snapshot",
		Outcome: model.OutcomeAccepted, RefID: verID,
		Decision: fmt.Sprintf("按 change_list 落新版本 article_version=%d", verID),
	})
	if cerr := storage.FinishRunOk(ctx, runID, verID); cerr != nil {
		return 0, reviews, cerr
	}
	return verID, reviews, nil
}
