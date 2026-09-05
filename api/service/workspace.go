package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"github.com/WWaynee/content-hub/storage"
	"github.com/WWaynee/content-hub/storage/model"
)

// 工作区 + 需求单业务层。

// RequirementInput 创建工作区时附带的需求单初步内容。
type RequirementInput struct {
	Title              string
	Tags               []string
	Platforms          []string
	StyleTone          string
	StyleEmotion       string
	StyleAudience      string
	StylePurpose       string
	StyleTaboo         string
	StyleSubject       string
	WordCount          int
	ChapterRequirement string
	// P10 draft_assist：起稿来源 + 用户粘贴的草稿原文（draft_assist 必填草稿，从零路径忽略）。
	SourceKind string
	DraftInput string
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

// RequirementCompletenessIssues 返回“能否一键生成”的缺项人话清单（空=已可生成）。
// 收敛口径（与前端 guide.requirementMissing 一致，RFC rev-4 W4）：需求单标题 + 发布平台 必填；
// 且（发文风格 或 字数 或 章节要求）至少一项；引用范围缺省=全部可访问资料，允许从零开始不在“缺项”里重申。
// 让前端据此禁用「生成」并在 tooltip 列出缺什么，同时作为后端 Generate 的前置硬校验(W4)，避免拿空/半需求真跑昂贵 LLM。
func RequirementCompletenessIssues(r *model.Requirement) []string {
	if r == nil {
		return []string{"需求单尚未存在"}
	}
	var missing []string
	if strings.TrimSpace(r.Title) == "" {
		missing = append(missing, "标题")
	}
	var ps []string
	_ = json.Unmarshal(r.Platforms, &ps)
	if len(ps) == 0 {
		missing = append(missing, "发布平台")
	}
	hasSpec := r.StyleTone != "" || r.StyleEmotion != "" || r.StyleAudience != "" || r.StylePurpose != "" ||
		r.StyleTaboo != "" || r.StyleSubject != "" || r.ChapterRequirement != "" || r.WordCount > 0
	if !hasSpec {
		missing = append(missing, "发文风格 / 字数 / 章节要求（至少其一）")
	}
	return missing
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
		r.SourceKind = reqIn.SourceKind
		r.DraftInput = reqIn.DraftInput
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
