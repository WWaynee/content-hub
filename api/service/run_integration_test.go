package service

import (
	"context"
	"errors"
	"testing"

	"github.com/WWaynee/content-hub/agent/coordinator"
	"github.com/WWaynee/content-hub/config"
	"github.com/WWaynee/content-hub/storage"
	"github.com/WWaynee/content-hub/storage/model"
)

// P05 run/step 生命周期与同 ws 排他(确定性路径)集成验收(真 MySQL，不触外部 LLM)。
// 竞争态的"两颗先后穿 active 检查、落版本时由 P02 版本 CAS 拦成只 +1"已被
// version_conflict_integration_test 覆盖；本测试补 run 记录的"active 拦截 / 建步 / 成功释放 / 失败释放"闭环。
func TestRunLifecycleAndActiveExclusion(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("配置加载失败: %v", err)
	}
	if _, err := storage.InitMySQL(&cfg.MySQL); err != nil {
		t.Skipf("MySQL 不可用: %v", err)
	}
	ctx := context.Background()
	tenantID := uint64(99990718)

	w, werr := CreateWorkspace(ctx, tenantID, 7, "P05 run 测试", nil)
	if werr != nil {
		t.Fatalf("create workspace: %v", werr)
	}
	co := coordinator.New()

	// 1. run A running + 未结束
	a, err := co.Start(ctx, coordinator.StartReq{
		TenantID: tenantID, UserID: 7, WorkspaceID: w.ID,
		RunType: model.RunInitial, BaseVersion: 0, CurrentRole: "planner",
	})
	if err != nil {
		t.Fatalf("start A: %v", err)
	}
	if a.Status != string(model.RunRunning) || !a.Active {
		t.Fatalf("A 应为 running+active，实得 status=%s active=%v", a.Status, a.Active)
	}

	// 2. A 未结束时再发第二个同 ws run → active 拒(NOT via race，确定性单线程)
	if _, err := co.Start(ctx, coordinator.StartReq{
		TenantID: tenantID, UserID: 7, WorkspaceID: w.ID,
		RunType: model.RunRevision, BaseVersion: 0, CurrentRole: "writer",
	}); !errors.Is(err, storage.ErrRunActive) {
		t.Fatalf("同 ws 有 active run 时应拒第二个，实得 %v", err)
	}

	// 3. 建步 + 读取
	if _, err := storage.AppendStep(ctx, a.ID, model.AgentStep{
		Role: model.RolePlanner, Action: "start", Successor: model.RoleRetriever, Outcome: model.OutcomeAccepted,
		Decision: "生成开始",
	}); err != nil {
		t.Fatalf("append step: %v", err)
	}
	steps, err := storage.ListSteps(ctx, a.ID)
	if err != nil || len(steps) != 1 {
		t.Fatalf("应有 1 步(直接 coordinator.Start 未预写步)，实得 %d steps err=%v", len(steps), err)
	}
	if steps[0].StepNo != 1 {
		t.Fatalf("step_no 应自 1 起：%v", steps[0].StepNo)
	}

	// 4. success 释放 active 后可再开：再次开始同 ws(此时无 active)→ 应能建
	if err := co.Success(ctx, tenantID, a.ID, 0); err != nil {
		t.Fatalf("A success: %v", err)
	}
	got, _ := storage.GetRun(ctx, tenantID, a.ID)
	if got.Status != string(model.RunSuccess) || got.Active {
		t.Fatalf("A 结束应 success+非 active")
	}

	b, err := co.Start(ctx, coordinator.StartReq{
		TenantID: tenantID, UserID: 7, WorkspaceID: w.ID,
		RunType: model.RunRevision, BaseVersion: 1, CurrentRole: "writer",
	})
	if err != nil {
		t.Fatalf("A 释放后再开 B 应成功: %v", err)
	}

	// 5. B 失败 → failed + 释放 active（让下一个能续）
	if err := co.Fail(ctx, b.ID, "人工取消场景/模拟失败"); err != nil {
		t.Fatalf("B fail: %v", err)
	}
	gotB, _ := storage.GetRun(ctx, tenantID, b.ID)
	if gotB.Status != string(model.RunFailed) || gotB.Active || gotB.ErrorMsg == "" {
		t.Fatalf("B 应 failed+非 active+有 error_msg：%+v", gotB)
	}
	// 失败释放后又能开下一个
	if _, err := co.Start(ctx, coordinator.StartReq{
		TenantID: tenantID, UserID: 7, WorkspaceID: w.ID,
		RunType: model.RunAppend, BaseVersion: 1, CurrentRole: "writer",
	}); err != nil && !errors.Is(err, storage.ErrRunActive) {
		t.Fatalf("失败释放后应可再开，实得 %v", err)
	}

	// 清理
	db := storage.GetDB()
	db.Exec("DELETE FROM agent_steps WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM agent_runs WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM article_versions WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM article_sentences WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM articles WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM workspaces WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM requirements WHERE tenant_id = ?", tenantID)
}
