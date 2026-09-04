package handler

import (
	"errors"
	"strings"

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
	ReqTitle           string   `json:"req_title,omitempty"`                    // 需求单标题
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
	// P10 draft_assist：起稿来源（build_from_scratch | draft_assist，缺省 build_from_scratch）；
	// draft_assist 模式要求 draft_input（用户自带的草稿原文）非空。
	SourceKind string `json:"source_kind,omitempty"`
	DraftInput string `json:"draft_input,omitempty"`
}

// CreateWorkspace 新建工作区（须携带需求单初步内容：需求单标题+平台，且风格/字数/章节至少一项；
// draft_assist 模式放宽：用户带自己的草稿起稿，需求单细化可后补，只要求草稿非空）。
func CreateWorkspace(c *gin.Context) {
	var req CreateWorkspaceReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误")
		return
	}
	if req.SourceKind == "" {
		req.SourceKind = "build_from_scratch"
	}
	if req.SourceKind != "build_from_scratch" && req.SourceKind != "draft_assist" {
		response.BadRequest(c, "source_kind 仅支持 build_from_scratch / draft_assist")
		return
	}
	// draft_assist：需求单标题缺省回退到工作区标题；放宽需求单初步内容校验（草稿优先，其余字段详情页可补）。
	if req.SourceKind == "draft_assist" {
		if strings.TrimSpace(req.DraftInput) == "" {
			response.BadRequest(c, "请粘贴你的草稿正文（draft_input 不能为空）")
			return
		}
		if strings.TrimSpace(req.ReqTitle) == "" {
			req.ReqTitle = req.Title
		}
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
		SourceKind:         req.SourceKind,
		DraftInput:         req.DraftInput,
	}
	if req.SourceKind == "build_from_scratch" && !reqIn.HasInitialContent() {
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
	response.Success(c, gin.H{"id": w.ID, "title": w.Title, "status": w.Status, "source_kind": req.SourceKind})
}

// ListWorkspaces 列出工作区（支持 title/status/tag/platform 检索）。
func ListWorkspaces(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	userID := middleware.GetUserID(c)
	title := c.Query("title")
	tag := c.Query("tag")
	platform := c.Query("platform")
	sort := c.Query("sort")

	// status 支持多值：?status=a&status=b 或逗号分隔 ?status=a,b
	var statuses []string
	for _, sv := range c.QueryArray("status") {
		for _, part := range strings.Split(sv, ",") {
			if part = strings.TrimSpace(part); part != "" {
				statuses = append(statuses, part)
			}
		}
	}

	f := storage.ListWorkspacesFilters{Title: title, Statuses: statuses, Tag: tag, Platform: platform, Sort: sort}
	list, err := storage.ListWorkspacesFiltered(c.Request.Context(), tenantID, userID, f)
	if err != nil {
		response.ServerError(c, "查询工作区失败")
		return
	}
	response.Success(c, list)
}

// DraftAssistArticleReq 从草稿起稿请求体（text 可选：缺省使用需求单已存的 draft_input）。
type DraftAssistArticleReq struct {
	Text string `json:"text,omitempty"`
}

// DraftAssistArticle 对 draft_assist 工作区执行"拿来稿起稿"（切分→治理→落首版）。
func DraftAssistArticle(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	userID := middleware.GetUserID(c)
	wid, err := parseID(c.Param("workspace_id"))
	if err != nil {
		response.BadRequest(c, "无效工作区 ID")
		return
	}
	var body DraftAssistArticleReq
	_ = c.ShouldBindJSON(&body) // body 可缺省（走需求单存的 draft_input）
	verID, reviews, serr := service.RunDraftAssist(c.Request.Context(), tenantID, userID, wid, body.Text)
	if serr != nil {
		switch {
		case errors.Is(serr, service.ErrDraftEmpty):
			response.BadRequest(c, serr.Error())
		case errors.Is(serr, service.ErrDraftAssistOnlyOnce):
			response.BadRequest(c, serr.Error())
		case errors.Is(serr, service.ErrRunActive):
			response.Fail(c, response.CodeServerError, service.ErrRunActive.Error())
		case errors.Is(serr, service.ErrArticleVersionConflict):
			response.Fail(c, response.CodeVersionConflict, service.ErrArticleVersionConflict.Error())
		default:
			response.ServerError(c, "从草稿起稿失败："+serr.Error())
		}
		return
	}
	response.Success(c, gin.H{
		"article_version_id": verID,
		"reviews":            reviews,
		"human_text":         buildDraftAssistHuman(reviews),
	})
}

// buildDraftAssistHuman 把起稿结果转成给用户的一句/几条人话。
func buildDraftAssistHuman(reviews []string) string {
	if len(reviews) == 0 {
		return "已把你的草稿整理成新稿件：全部句子都能在知识库中找到可引用来源，或属于无需外部依据的表述。"
	}
	return "已把你的草稿整理成新稿件并标注每一句的来源。注意：\n- " + strings.Join(reviews, "\n- ")
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
