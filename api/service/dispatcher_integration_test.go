//go:build integration

package service

import (
	"context"
	"testing"

	"github.com/WWaynee/content-hub/config"
	"github.com/WWaynee/content-hub/storage"
)

// TestDispatcherUpdateRequirement 验证：对话"改基调"→ 解析 → update_requirement_field 真正落库。
func TestDispatcherUpdateRequirement(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("配置加载失败: %v", err)
	}
	if cfg.LLM.APIKey == "" {
		t.Skip("LLM 未配置，跳过")
	}
	if _, err := storage.InitMySQL(&cfg.MySQL); err != nil {
		t.Skipf("MySQL 不可用: %v", err)
	}
	ctx := context.Background()
	tenantID := uint64(99990070)

	w, _ := CreateWorkspace(ctx, tenantID, 1, "对话测试", nil)
	req, _ := storage.GetRequirementByWorkspace(ctx, tenantID, w.ID)

	d := NewDispatcher()
	res, err := d.ProcessChat(ctx, tenantID, 1, w.ID, "把写作基调改成正式", "requirement_field", req.ID)
	if err != nil {
		t.Fatalf("ProcessChat 失败: %v", err)
	}
	if len(res.Results) == 0 {
		t.Fatal("应有 action 执行结果")
	}
	t.Logf("动作计划 %d 个 action，结果: %+v", len(res.Plan.Actions), res.Results)

	// 验证需求单 style_tone 被更新 + version 递增
	updated, _ := storage.GetRequirementByID(ctx, req.ID)
	if updated.StyleTone != "正式" {
		t.Errorf("style_tone 应被对话更新为正式，实际=%q", updated.StyleTone)
	}
	if updated.Version <= req.Version {
		t.Errorf("version 应递增，改前=%d 改后=%d", req.Version, updated.Version)
	}

	// 清理
	db := storage.GetDB()
	db.Exec("DELETE FROM conversation_messages WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM conversations WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM requirements WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM workspaces WHERE tenant_id = ?", tenantID)
}
