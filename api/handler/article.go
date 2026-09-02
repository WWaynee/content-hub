package handler

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/WWaynee/content-hub/agent"
	agentcensor "github.com/WWaynee/content-hub/agent/censor"
	"github.com/WWaynee/content-hub/agent/evidence"
	"github.com/WWaynee/content-hub/agent/orchestrator"
	"github.com/WWaynee/content-hub/agent/retrieve"
	"github.com/WWaynee/content-hub/agent/writing"
	"github.com/WWaynee/content-hub/api/middleware"
	"github.com/WWaynee/content-hub/api/response"
	"github.com/WWaynee/content-hub/api/service"
	"github.com/WWaynee/content-hub/llmclient"
	"github.com/WWaynee/content-hub/storage"
	"github.com/WWaynee/content-hub/storage/model"
)

// 稿件 HTTP handler。

// GenerateArticle 触发稿件生成（generation：检索→撰写→证据→落快照）。
func GenerateArticle(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	userID := middleware.GetUserID(c)
	wid, err := parseID(c.Param("workspace_id"))
	if err != nil {
		response.BadRequest(c, "无效工作区 ID")
		return
	}
	req, err := storage.GetRequirementByWorkspace(c.Request.Context(), tenantID, wid)
	if err != nil {
		response.BadRequest(c, "需求单不存在")
		return
	}
	agentReq := toAgentRequirement(req)

	// 记录进入"生成中"前的工作区状态，供失败时回退（避免卡在 generating）
	var prevStatus string
	if ws, werr := storage.GetWorkspaceByID(c.Request.Context(), tenantID, userID, wid); werr == nil {
		prevStatus = ws.Status
	}

	// 状态机：进入生成中（禁导）
	_ = storage.UpdateWorkspaceStatus(c.Request.Context(), wid, "generating")

	// 勾选范围 → 递归展开为文件 ID，锁定检索范围（无勾选则 nil=全租户）
	fileIDs, err := service.RequirementFileIDScope(c.Request.Context(), tenantID, req.ID)
	if err != nil {
		restoreWorkspaceStatus(c, wid, prevStatus)
		response.ServerError(c, "展开勾选范围失败："+err.Error())
		return
	}

	llm := llmclient.NewClient()
	checker := agentcensor.NewClaimPlanner(llm, service.NewKbaseSearcher())
	o := orchestrator.New(retrieve.New(llm), writing.New(llm), evidence.New(), checker).
		SetFactVerifier(agentcensor.NewFactVerifier(llm))
	res, err := o.Generate(c.Request.Context(), tenantID, agentReq, fileIDs)
	if err != nil {
		restoreWorkspaceStatus(c, wid, prevStatus)
		var insuff *orchestrator.ErrInsufficientEvidence
		var factUnsup *orchestrator.ErrFactUnsupported
		switch {
		case errors.As(err, &insuff):
			response.Fail(c, response.CodeServerError, buildNoEvidenceMessage(insuff))
			return
		case errors.As(err, &factUnsup):
			response.Fail(c, response.CodeServerError,
				"稿件未通过事实校验，部分数据在知识库中无证据支撑，已禁止生成：\n"+buildUnsupportedMessage(factUnsup))
			return
		case errors.Is(err, orchestrator.ErrNoEvidence):
			response.Fail(c, response.CodeServerError,
				"知识库中未检索到与该需求主题相关的资料，无法生成含具体数据的稿件。请先在知识库补充相关文档资料，或调整需求单内容后重试。")
			return
		default:
			response.ServerError(c, "生成失败："+err.Error())
			return
		}
	}

	// 检索快照落库（供惰性失效判定 + 证据追溯）；失败不阻断主流程
	if _, berr := service.PersistRetrievalBatch(c.Request.Context(), tenantID, wid, req.ID, req.Version, res.Queries, service.EvidenceToKbaseHits(res.Evidence)); berr != nil {
		_ = berr
	}

	verID, err := service.PersistArticleSnapshot(c.Request.Context(), tenantID, wid, res.Article, res.Evidence)
	if err != nil {
		restoreWorkspaceStatus(c, wid, prevStatus)
		if errors.Is(err, service.ErrArticleVersionConflict) {
			response.Fail(c, response.CodeVersionConflict, service.ErrArticleVersionConflict.Error())
			return
		}
		response.ServerError(c, "稿件落库失败："+err.Error())
		return
	}
	storage.UpdateWorkspaceStatus(c.Request.Context(), wid, "generated")

	response.Success(c, gin.H{"article_version_id": verID})
}

// restoreWorkspaceStatus 生成失败时回退工作区状态（保持可操作，不卡死在 generating）。
func restoreWorkspaceStatus(c *gin.Context, wid uint64, prevStatus string) {
	if prevStatus == "" {
		prevStatus = "draft"
	}
	_ = storage.UpdateWorkspaceStatus(c.Request.Context(), wid, prevStatus)
}

// buildNoEvidenceMessage 把缺证清单组装成给用户的明确提示。
func buildNoEvidenceMessage(insuff *orchestrator.ErrInsufficientEvidence) string {
	var sb strings.Builder
	sb.WriteString("以下内容在知识库中未检索到可支撑的资料，无法生成含这些数据的稿件：")
	for _, m := range insuff.Missing {
		sb.WriteString("\n· " + m.Text)
	}
	sb.WriteString("\n请补充相关文档资料，或调整需求单后重试。")
	return sb.String()
}

// buildUnsupportedMessage 把无法获得证据支撑的数据断言清单组装成提示。
func buildUnsupportedMessage(unsup *orchestrator.ErrFactUnsupported) string {
	var sb strings.Builder
	sb.WriteString("存在无法在知识库证据原文中找到支撑的数据断言：")
	for _, u := range unsup.Unsupported {
		sb.WriteString("\n· " + u)
	}
	sb.WriteString("\n请补充相关资料，或调整需求后重试。")
	return sb.String()
}

// GetArticle 读取稿件（含句子 + 证据标注）。
func GetArticle(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	wid, err := parseID(c.Param("workspace_id"))
	if err != nil {
		response.BadRequest(c, "无效工作区 ID")
		return
	}
	a, err := storage.GetArticleByWorkspace(c.Request.Context(), tenantID, wid)
	if err != nil {
		response.BadRequest(c, "稿件不存在")
		return
	}
	ver, err := storage.GetLatestArticleVersion(c.Request.Context(), a.ID)
	if err != nil {
		response.BadRequest(c, "稿件版本不存在")
		return
	}
	sents, _ := storage.ListArticleSentences(c.Request.Context(), ver.ID)
	binds, _ := storage.ListArticleBindings(c.Request.Context(), ver.ID)

	response.Success(c, gin.H{
		"article_id":         a.ID,
		"article_version_id": ver.ID,
		"title":              a.Title,
		"full_content":       ver.FullContent,
		"sentences":          sents,
		"bindings":           binds,
	})
}

// ExportArticle 导出稿件（合并 md）。
func ExportArticle(c *gin.Context) {
	vid, err := parseID(c.Param("article_version_id"))
	if err != nil {
		response.BadRequest(c, "无效稿件版本 ID")
		return
	}
	md, err := service.ExportArticle(c.Request.Context(), vid)
	if err != nil {
		response.ServerError(c, "导出失败："+err.Error())
		return
	}
	response.Success(c, gin.H{"markdown": md})
}

func toAgentRequirement(r *model.Requirement) agent.Requirement {
	return agent.Requirement{
		Title:              r.Title,
		Platforms:          jsonToStringSlice(r.Platforms),
		StyleTone:          r.StyleTone,
		StyleEmotion:       r.StyleEmotion,
		StyleAudience:      r.StyleAudience,
		StylePurpose:       r.StylePurpose,
		StyleTaboo:         r.StyleTaboo,
		StyleSubject:       r.StyleSubject,
		WordCount:          r.WordCount,
		ChapterRequirement: r.ChapterRequirement,
	}
}

// jsonToStringSlice 把 datatypes.JSON 解析为 []string。
func jsonToStringSlice(j interface{}) []string {
	b, err := json.Marshal(j)
	if err != nil {
		return nil
	}
	var out []string
	_ = json.Unmarshal(b, &out)
	return out
}
