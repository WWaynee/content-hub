package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"github.com/WWaynee/content-hub/storage"
	"github.com/WWaynee/content-hub/storage/model"
)

// 工作区 + 需求单业务层。

// RequirementInput 创建工作区时附带的需求单初步内容。
type RequirementInput struct {
	Title               string
	Tags                []string
	Platforms           []string
	StyleTone           string
	StyleEmotion        string
	StyleAudience       string
	StylePurpose        string
	StyleTaboo          string
	StyleSubject        string
	WordCount           int
	ChapterRequirement  string
}

// ErrRequirementIncomplete 需求单初步内容不完整。
var ErrRequirementIncomplete = errors.New("需求单初步内容不完整")

// HasInitialContent 判断需求单是否具备"初步内容"（需求单标题/平台必填，且风格或字数或章节至少一项）。
func (r *RequirementInput) HasInitialContent() bool {
	if r == nil {
		return false
	}
	if r.Title == "" || len(r.Platforms) == 0 {
		return false
	}
	if r.StyleTone != "" || r.StyleEmotion != "" || r.StyleAudience != "" || r.StylePurpose != "" ||
		r.StyleTaboo != "" || r.StyleSubject != "" || r.WordCount > 0 || r.ChapterRequirement != "" {
		return true
	}
	return false
}

// CreateWorkspace 创建工作区（一个工作区对应一个需求单）。reqIn 可携带需求单初步内容，
// 若传入则要求 mustComplete=true 时初步内容完整（由 handler 决定）。
func CreateWorkspace(ctx context.Context, tenantID, ownerUserID uint64, title string, reqIn *RequirementInput) (*model.Workspace, error) {
	w := &model.Workspace{
		TenantID:    tenantID,
		OwnerUserID: ownerUserID,
		Title:       title,
		Status:      "draft",
	}
	r := &model.Requirement{
		WorkspaceID: w.ID,
		TenantID:    tenantID,
		Title:       title,
		Version:     1,
	}
	// 若带需求单初步内容，填充到需求单
	if reqIn != nil {
		r.Title = reqIn.Title
		if len(reqIn.Tags) > 0 {
			b, _ := json.Marshal(reqIn.Tags)
			r.Tags = b
		}
		if len(reqIn.Platforms) > 0 {
			b, _ := json.Marshal(reqIn.Platforms)
			r.Platforms = b
		}
		r.StyleTone = reqIn.StyleTone
		r.StyleEmotion = reqIn.StyleEmotion
		r.StyleAudience = reqIn.StyleAudience
		r.StylePurpose = reqIn.StylePurpose
		r.StyleTaboo = reqIn.StyleTaboo
		r.StyleSubject = reqIn.StyleSubject
		r.WordCount = reqIn.WordCount
		r.ChapterRequirement = reqIn.ChapterRequirement
	}

	err := storage.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(w).Error; err != nil {
			return fmt.Errorf("创建工作区失败: %w", err)
		}
		r.WorkspaceID = w.ID
		r.TenantID = tenantID
		if err := tx.Create(r).Error; err != nil {
			return fmt.Errorf("创建需求单失败: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
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
