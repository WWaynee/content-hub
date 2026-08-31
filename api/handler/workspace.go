package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/WWaynee/content-hub/api/middleware"
	"github.com/WWaynee/content-hub/api/response"
	"github.com/WWaynee/content-hub/api/service"
	"github.com/WWaynee/content-hub/storage"
)

// 工作区 HTTP handler。

// CreateWorkspaceReq 新建工作区请求（可携带需求单初步内容）。
type CreateWorkspaceReq struct {
	Title              string   `json:"title" binding:"required,min=1,max=256"` // 工作区标题必填
	ReqTitle           string   `json:"req_title,omitempty"`                     // 需求单标题
	Tags               []string `json:"tags,omitempty"`
	Platforms          []string `json:"platforms,omitempty"`
	StyleTone          string   `json:"style_tone,omitempty"`
	StyleEmotion       string   `json:"style_emotion,omitempty"`
	StyleAudience      string   `json:"style_audience,omitempty"`
	StylePurpose       string   `json:"style_purpose,omitempty"`
	StyleTaboo         string   `json:"style_taboo,omitempty"`
	StyleSubject       string   `json:"style_subject,omitempty"`
	WordCount          int      `json:"word_count,omitempty"`
	ChapterRequirement string   `json:"chapter_requirement,omitempty"`
}

// CreateWorkspace 新建工作区（必须提供需求单初步内容：需求单标题+平台，且风格/字数/章节至少一项）。
func CreateWorkspace(c *gin.Context) {
	var req CreateWorkspaceReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误")
		return
	}
	reqIn := &service.RequirementInput{
		Title:              req.ReqTitle,
		Tags:               req.Tags,
		Platforms:          req.Platforms,
		StyleTone:          req.StyleTone,
		StyleEmotion:       req.StyleEmotion,
		StyleAudience:      req.StyleAudience,
		StylePurpose:       req.StylePurpose,
		StyleTaboo:         req.StyleTaboo,
		StyleSubject:       req.StyleSubject,
		WordCount:          req.WordCount,
		ChapterRequirement: req.ChapterRequirement,
	}
	if !reqIn.HasInitialContent() {
		response.BadRequest(c, "请填写需求单初步内容：需求单标题、发布平台，且风格或字数或章节至少一项")
		return
	}
	tenantID := middleware.GetTenantID(c)
	userID := middleware.GetUserID(c)
	w, err := service.CreateWorkspace(c.Request.Context(), tenantID, userID, req.Title, reqIn)
	if err != nil {
		response.ServerError(c, "新建工作区失败")
		return
	}
	response.Success(c, gin.H{"id": w.ID, "title": w.Title, "status": w.Status})
}

// ListWorkspaces 列出工作区（支持 title/status/tag/platform 检索）。
func ListWorkspaces(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	userID := middleware.GetUserID(c)
	title := c.Query("title")
	status := c.Query("status")
	tag := c.Query("tag")
	platform := c.Query("platform")

	list, err := storage.ListWorkspacesFiltered(c.Request.Context(), tenantID, userID, title, status, tag, platform)
	if err != nil {
		response.ServerError(c, "查询工作区失败")
		return
	}
	response.Success(c, list)
}

// DeleteWorkspace 软删除工作区（仅本人）。
func DeleteWorkspace(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	userID := middleware.GetUserID(c)
	var id uint64
	if _, err := fmtSscanfID(c.Param("id"), &id); err != nil {
		response.BadRequest(c, "无效的工作区 ID")
		return
	}
	if err := storage.SoftDeleteWorkspace(c.Request.Context(), tenantID, userID, id); err != nil {
		response.ServerError(c, "删除失败")
		return
	}
	response.SuccessMessage(c, "已删除", nil)
}
