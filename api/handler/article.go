package handler

import (
	"encoding/json"

	"github.com/gin-gonic/gin"

	"github.com/WWaynee/content-hub/agent"
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

	// 状态机：进入生成中（禁导）
	storage.UpdateWorkspaceStatus(c.Request.Context(), wid, "generating")

	// 勾选范围 → 递归展开为文件 ID，锁定检索范围（无勾选则 nil=全租户）
	fileIDs, err := service.RequirementFileIDScope(c.Request.Context(), tenantID, req.ID)
	if err != nil {
		response.ServerError(c, "展开勾选范围失败："+err.Error())
		return
	}

	llm := llmclient.NewClient()
	o := orchestrator.New(retrieve.New(llm), writing.New(llm), evidence.New())
	res, err := o.Generate(c.Request.Context(), tenantID, agentReq, fileIDs)
	if err != nil {
		response.ServerError(c, "生成失败："+err.Error())
		return
	}

	// 检索快照落库（供惰性失效判定 + 证据追溯）；失败不阻断主流程
	if _, berr := service.PersistRetrievalBatch(c.Request.Context(), tenantID, wid, req.ID, req.Version, res.Queries, service.EvidenceToKbaseHits(res.Evidence)); berr != nil {
		_ = berr
	}

	verID, err := service.PersistArticleSnapshot(c.Request.Context(), tenantID, wid, req.Version, res.Article, res.Evidence)
	if err != nil {
		response.ServerError(c, "稿件落库失败："+err.Error())
		return
	}
	storage.UpdateWorkspaceStatus(c.Request.Context(), wid, "generated")

	response.Success(c, gin.H{"article_version_id": verID})
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
