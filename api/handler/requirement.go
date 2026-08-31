package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/WWaynee/content-hub/api/middleware"
	"github.com/WWaynee/content-hub/api/response"
	"github.com/WWaynee/content-hub/storage"
	"github.com/WWaynee/content-hub/storage/model"
)

// 需求单 HTTP handler。

// GetRequirement 读取工作区对应的需求单。
func GetRequirement(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	wid, err := parseID(c.Param("workspace_id"))
	if err != nil {
		response.BadRequest(c, "无效的工作区 ID")
		return
	}
	req, err := storage.GetRequirementByWorkspace(c.Request.Context(), tenantID, wid)
	if err != nil {
		response.ServerError(c, "查询需求单失败")
		return
	}
	response.Success(c, req)
}

// UpdateRequirementReq 手动保存需求单请求（全字段，不含 version）。
type UpdateRequirementReq struct {
	Title             string   `json:"title"`
	Tags              []string `json:"tags"`
	Platforms         []string `json:"platforms"`
	StyleTone         string   `json:"style_tone"`
	StyleEmotion      string   `json:"style_emotion"`
	StyleAudience     string   `json:"style_audience"`
	StylePurpose      string   `json:"style_purpose"`
	StyleTaboo        string   `json:"style_taboo"`
	StyleSubject      string   `json:"style_subject"`
	WordCount         int      `json:"word_count"`
	ChapterRequirement string  `json:"chapter_requirement"`
}

// UpdateRequirement 手动保存需求单（全字段 + version 递增）。
func UpdateRequirement(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	rid, err := parseID(c.Param("id"))
	if err != nil {
		response.BadRequest(c, "无效的需求单 ID")
		return
	}
	// 校验归属
	req, err := storage.GetRequirementByID(c.Request.Context(), rid)
	if err != nil || req.TenantID != tenantID {
		response.BadRequest(c, "需求单不存在")
		return
	}

	var body UpdateRequirementReq
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, "参数错误")
		return
	}

	fields := map[string]interface{}{
		"title":              body.Title,
		"style_tone":         body.StyleTone,
		"style_emotion":      body.StyleEmotion,
		"style_audience":     body.StyleAudience,
		"style_purpose":      body.StylePurpose,
		"style_taboo":        body.StyleTaboo,
		"style_subject":      body.StyleSubject,
		"word_count":         body.WordCount,
		"chapter_requirement": body.ChapterRequirement,
	}
	// 处理 tags/platforms JSON
	tagsJSON, _ := jsonMarshal(body.Tags)
	platformsJSON, _ := jsonMarshal(body.Platforms)
	fields["tags"] = tagsJSON
	fields["platforms"] = platformsJSON

	if err := storage.UpdateRequirement(c.Request.Context(), rid, fields); err != nil {
		response.ServerError(c, "保存失败")
		return
	}
	updated, _ := storage.GetRequirementByID(c.Request.Context(), rid)
	response.Success(c, updated)
}

// SaveRequirementScopeReq 保存勾选范围请求。
type SaveRequirementScopeReq struct {
	Scopes []ScopeItem `json:"scopes"`
}

// ScopeItem 单个引用范围（dir 或 file）。
type ScopeItem struct {
	ScopeType  string `json:"scope_type"`  // public/private
	TargetType string `json:"target_type"` // dir/file
	DirID      uint64 `json:"dir_id"`
	FileID     uint64 `json:"file_id"`
}

// SaveRequirementScope 保存勾选范围。
func SaveRequirementScope(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	rid, err := parseID(c.Param("id"))
	if err != nil {
		response.BadRequest(c, "无效的需求单 ID")
		return
	}
	req, err := storage.GetRequirementByID(c.Request.Context(), rid)
	if err != nil || req.TenantID != tenantID {
		response.BadRequest(c, "需求单不存在")
		return
	}
	var body SaveRequirementScopeReq
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, "参数错误")
		return
	}
	scopes := make([]model.RequirementScope, 0, len(body.Scopes))
	for _, s := range body.Scopes {
		scopes = append(scopes, model.RequirementScope{
			ScopeType:  s.ScopeType,
			TargetType: s.TargetType,
			DirID:      s.DirID,
			FileID:     s.FileID,
		})
	}
	if err := storage.SetRequirementScopes(c.Request.Context(), rid, tenantID, scopes); err != nil {
		response.ServerError(c, "保存范围失败")
		return
	}
	// 范围变更也需递增 version（核心范围字段变更触发失效）
	_ = storage.BumpRequirementVersion(c.Request.Context(), rid)
	response.SuccessMessage(c, "已保存", nil)
}
