package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/WWaynee/content-hub/storage/model"
)

// agent run/step 存储层（P05）。
//
// 并发语义：agent_runs 的 active 排他用于"可见进行中状态/await_human(P06)"；
// 稿件真正的一致性由文章版本 CAS(P02：current_version_no: base→base+1)兜底——
// 即便两个 run 同时穿过 active 检查，较晚到版本 CAS 的一方也会被拒绝而不写坏版本，
// 它的 run 会被标记 failed（释放 active），从而"第二个因 active 或版本 CAS 被拒"两种路都可收敛。

var ErrRunActive = errors.New("该工作区已有进行中(或等待人工)的生成任务，请勿重复发起")

// BeginRun 开启一个新的 agent run。返回 (runID, installed?)。installed=false 表示因同 ws 有活跃 run 而未建。
// 同 ws 无 active 时建 run。base：该 run 基于的稿件版本号。
func BeginRun(ctx context.Context, tenantID, userID, workspaceID uint64, runType string, baseVersion int, planJSON string, currentRole string) (*model.AgentRun, error) {
	var runID uint64
	err := GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var active int64
		if err := tx.Model(&model.AgentRun{}).
			Where("workspace_id = ? AND tenant_id = ? AND active = ? AND status IN ?",
				workspaceID, tenantID, true, []string{string(model.RunRunning), string(model.RunAwaitingHuman)}).
			Count(&active).Error; err != nil {
			return err
		}
		if active > 0 {
			return ErrRunActive
		}
		// MySQL JSON 列对"空串"会校验报 Invalid JSON：无 plan 时存合法值 'null'（RFC 语义仍是可 NULL/未填）
	if strings.TrimSpace(planJSON) == "" {
		planJSON = "null"
	}
	r := &model.AgentRun{
			TenantID:           tenantID,
			UserID:             userID,
			WorkspaceID:        workspaceID,
			RunType:            runType,
			BaseArticleVersion: baseVersion,
			Status:             string(model.RunRunning),
			Active:             true,
			Plan:               planJSON,
			CurrentRole:        currentRole,
			CurrentAction:      "start",
		}
		if err := tx.Create(r).Error; err != nil {
			return err
		}
		runID = r.ID
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &model.AgentRun{ID: runID}, nil
}

// GetRun 取 run（tenant 限定）。
func GetRun(ctx context.Context, tenantID, runID uint64) (*model.AgentRun, error) {
	var r model.AgentRun
	if err := GetDB().WithContext(ctx).Where("id = ? AND tenant_id = ?", runID, tenantID).First(&r).Error; err != nil {
		return nil, err
	}
	return &r, nil
}

// ListActiveRun 取某 ws 当前进行中/等待人工、可见的 run（可能不存在）。
func ListActiveRun(ctx context.Context, tenantID, workspaceID uint64) (*model.AgentRun, error) {
	var r model.AgentRun
	if err := GetDB().WithContext(ctx).
		Where("workspace_id = ? AND tenant_id = ? AND status IN ?",
			workspaceID, tenantID, []string{string(model.RunRunning), string(model.RunAwaitingHuman)}).
		Order("id ASC").First(&r).Error; err != nil {
		return nil, err // gorm.ErrRecordNotFound 由调用方判断
	}
	return &r, nil
}

// ListRunsByWorkspace 列出某 ws 全部 run（审计）。
func ListRunsByWorkspace(ctx context.Context, tenantID, workspaceID uint64) ([]model.AgentRun, error) {
	var list []model.AgentRun
	if err := GetDB().WithContext(ctx).Where("workspace_id = ? AND tenant_id = ?", workspaceID, tenantID).
		Order("id DESC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// UpdateRunProgress 推进 run 的"人话状态"（current_role/current_action），不改变整体 status。
func UpdateRunProgress(ctx context.Context, runID uint64, role, action string) error {
	return GetDB().WithContext(ctx).Model(&model.AgentRun{}).Where("id = ?", runID).
		Updates(map[string]interface{}{"current_role": role, "current_action": action, "updated_at": time.Now()}).Error
}

// AppendStep 往某 run 追加一步（step_no = 该 run 现有 max+1）。
func AppendStep(ctx context.Context, runID uint64, step model.AgentStep) (uint64, error) {
	var stepID uint64
	err := GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 取 run 以注入 tenant/workspace，并取下一 step_no
		var r model.AgentRun
		if err := tx.Where("id = ?", runID).First(&r).Error; err != nil {
			return err
		}
		var maxNo int
		tx.Model(&model.AgentStep{}).Where("run_id = ?", runID).Select("COALESCE(MAX(step_no),0)").Scan(&maxNo)
		step.RunID = runID
		step.TenantID = r.TenantID
		step.WorkspaceID = r.WorkspaceID
		step.StepNo = maxNo + 1
		if err := tx.Create(&step).Error; err != nil {
			return err
		}
		stepID = step.ID
		return nil
	})
	if err != nil {
		return 0, err
	}
	return stepID, nil
}

// MarkAllStepsFinal 在 run 结束(成功/失败)时把仍标为进行中的进度步收口为 done，保证 DB 回放一致
//（不依赖每个 step_done 事件是否都成功更新同一行，兜底防断线/竞态漏标）。
func MarkAllStepsFinal(ctx context.Context, runID uint64) error {
	return GetDB().WithContext(ctx).Model(&model.AgentStep{}).Where("run_id = ? AND done = ?", runID, false).
		Updates(map[string]interface{}{"done": true, "updated_at": time.Now()}).Error
}

// ListSteps 列出某 run 的全部 step（按 step_no 升序，供回放/审计/前端轮询）。
func ListSteps(ctx context.Context, runID uint64) ([]model.AgentStep, error) {
	var list []model.AgentStep
	if err := GetDB().WithContext(ctx).Where("run_id = ?", runID).Order("step_no ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// BeginProgressStep 落一条"进行中"的 P13 进度步（done=false）。返回 step.id 供后续 EndProgressStep 收口。
// 它把语义步（用户可感知的检索/撰写/校验/整理）作为 run 内一行，StepNo 沿用 AppendStep 的自增次序，
// 让前端能"看到当前在跑哪一步"。区别于 start_generation 等引导行，真正的进度行 role 来自生成主链。
func BeginProgressStep(ctx context.Context, runID uint64, role, action, title string, total int) (uint64, error) {
	step := model.AgentStep{
		Role: role, Action: action, StepTitle: title,
		TotalSteps: total, Done: false, Outcome: model.OutcomeAccepted,
	}
	return AppendStep(ctx, runID, step)
}

// EndProgressStep 收口一步进度：置完成/失败、耗时与对 LLM/检索的发送-回执摘要，并返回该步。
func EndProgressStep(ctx context.Context, stepID uint64, done, failed bool, failure, detail string, durationMs int64) error {
	up := map[string]interface{}{
		"done": done, "failure": "", "detail": detail, "duration_ms": durationMs, "updated_at": time.Now(),
	}
	if failed {
		up["done"], up["outcome"] = true, model.OutcomeRejected
	}
	if failure != "" {
		up["failure"] = failure
	}
	return GetDB().WithContext(ctx).Model(&model.AgentStep{}).Where("id = ?", stepID).Updates(up).Error
}

// FinishRunOk 正常完成 run（成功并产出版本）。置 status=success，活省 active，记录 result_version_id。依赖 index idx_ws_active 支持。
func FinishRunOk(ctx context.Context, runID, resultVersionID uint64) error {
	res := GetDB().WithContext(ctx).Model(&model.AgentRun{}).Where("id = ?", runID).
		Updates(map[string]interface{}{"status": string(model.RunSuccess), "active": false,
			"result_version_id": resultVersionID, "current_action": "done", "updated_at": time.Now()})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("run %d 不存在或已结束", runID)
	}
	return nil
}

// FailRun 置 run 失败（释放 active，保留 reason 供 UI 显示"卡在哪、为什么"）。
func FailRun(ctx context.Context, runID uint64, reason string) error {
	res := GetDB().WithContext(ctx).Model(&model.AgentRun{}).Where("id = ?", runID).
		Updates(map[string]interface{}{"status": string(model.RunFailed), "active": false,
			"error_msg": reason, "current_action": "failed", "updated_at": time.Now()})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("run %d 不存在或已结束", runID)
	}
	return nil
}

// MarkAwaitingHuman 把 run 置"等待人工"(P06 ask_human)。仍保持 active=1（防止有人重复触发）。
func MarkAwaitingHuman(ctx context.Context, runID uint64, pendingRole, pendingAction string) error {
	return GetDB().WithContext(ctx).Model(&model.AgentRun{}).Where("id = ?", runID).
		Updates(map[string]interface{}{"status": string(model.RunAwaitingHuman), "active": true,
			"current_role": pendingRole, "current_action": pendingAction, "updated_at": time.Now()}).Error
}

// CancelRun 取消 run（幂等停止，含把进行中 run 置 cancelled）。
func CancelRun(ctx context.Context, runID uint64) error {
	res := GetDB().WithContext(ctx).Model(&model.AgentRun{}).Where("id = ?", runID).
		Updates(map[string]interface{}{"status": string(model.RunCancelled), "active": false,
			"current_action": "cancelled", "updated_at": time.Now()})
	if res.Error != nil {
		return res.Error
	}
	return nil
}
