package service

import (
	"context"
	"fmt"

	"github.com/WWaynee/content-hub/storage"
	"github.com/WWaynee/content-hub/storage/model"
)

// 工作区 + 需求单业务层。

// CreateWorkspace 创建工作区（一个工作区对应一个需求单）。
func CreateWorkspace(ctx context.Context, tenantID, ownerUserID uint64, title string) (*model.Workspace, error) {
	w := &model.Workspace{
		TenantID:    tenantID,
		OwnerUserID: ownerUserID,
		Title:       title,
		Status:      "draft",
	}
	if err := storage.CreateWorkspace(ctx, w); err != nil {
		return nil, fmt.Errorf("创建工作区失败: %w", err)
	}
	// 同时创建需求单（必填字段后续由前端/对话填充）
	r := &model.Requirement{
		WorkspaceID: w.ID,
		TenantID:    tenantID,
		Title:       title,
		Version:     1,
	}
	if err := storage.CreateRequirement(ctx, r); err != nil {
		return nil, fmt.Errorf("创建需求单失败: %w", err)
	}
	return w, nil
}

// UpdateRequirementField 更新需求单的某个可对话修改字段（白名单已由上层校验）。
// 返回更新后的需求单（含新 version）。
func UpdateRequirementField(ctx context.Context, requirementID uint64, field, value string) (*model.Requirement, error) {
	if err := storage.UpdateRequirement(ctx, requirementID, map[string]interface{}{field: value}); err != nil {
		return nil, fmt.Errorf("更新需求单字段失败: %w", err)
	}
	return storage.GetRequirementByID(ctx, requirementID)
}
