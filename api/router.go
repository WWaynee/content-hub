package api

import (
	"github.com/gin-gonic/gin"

	"github.com/WWaynee/content-hub/api/handler"
	"github.com/WWaynee/content-hub/api/middleware"
	"github.com/WWaynee/content-hub/observability"
)

// NewRouter 构建 Gin 路由。
// 中间件顺序：Recovery → CORS → Trace → Logger。
// 私有组挂：JWT 鉴权 + 限流。
func NewRouter() *gin.Engine {
	r := gin.New()
	r.Use(middleware.Recovery(), middleware.CORS(), middleware.Trace(), middleware.Logger())

	// 健康检查（公开）
	r.GET("/health", handler.Health)

	// Prometheus 指标（建议内网/鉴权，当前开发期公开）
	r.GET("/metrics", gin.WrapH(observability.MetricsHandler()))

	// 公开组：认证
	pub := r.Group("/api")
	{
		pub.POST("/tenant/register", handler.RegisterTenant)
		pub.POST("/user/login", handler.Login)
	}

	// 私有组：需登录
	priv := r.Group("/api")
	priv.Use(middleware.JWTAuth(), middleware.RateLimiter())
	{
		priv.POST("/user/register", handler.RegisterMember)
		priv.GET("/user/profile", handler.Profile)

		// 工作区
		priv.GET("/workspaces", handler.ListWorkspaces)
		priv.POST("/workspaces", handler.CreateWorkspace)
		priv.DELETE("/workspaces/:id", handler.DeleteWorkspace)

		// 需求单
		priv.GET("/workspaces/:workspace_id/requirement", handler.GetRequirement)
		priv.PUT("/requirements/:id", handler.UpdateRequirement)
		priv.PUT("/requirements/:id/scope", handler.SaveRequirementScope)

		// 知识库
		priv.GET("/kbase/dir", handler.ListKbaseDir)
		priv.GET("/kbase/tree", handler.GetKbaseTree)
		priv.POST("/kbase/dir", handler.CreateKbaseDir)
		priv.PUT("/kbase/dir/:id", handler.RenameKbaseDir)
		priv.DELETE("/kbase/dir/:id", handler.DeleteKbaseDir)
		priv.POST("/kbase/file", handler.UploadFile)
		priv.PUT("/kbase/file/:id", handler.RenameKbaseFile)
		priv.DELETE("/kbase/file/:id", handler.DeleteKbaseFile)
		priv.GET("/kbase/file/:id/preview", handler.PreviewFile)
		priv.GET("/kbase/file/:id/content", handler.GetFileContent)
		priv.GET("/kbase/file/:id/download", handler.DownloadFile)

		// 稿件
		priv.POST("/workspaces/:workspace_id/generate", handler.GenerateArticle)
		priv.GET("/workspaces/:workspace_id/article", handler.GetArticle)
		priv.GET("/articles/:article_version_id/export", handler.ExportArticle)
		priv.POST("/workspaces/:workspace_id/chat", handler.WorkspaceChat)

		// 知识库问答
		priv.GET("/qa/sessions", handler.ListQASessions)
		priv.POST("/qa/sessions", handler.CreateQASession)
		priv.PUT("/qa/sessions/:session_id", handler.RenameQASession)
		priv.DELETE("/qa/sessions/:session_id", handler.DeleteQASession)
		priv.POST("/qa/sessions/:session_id/ask", handler.AskQA)
		priv.GET("/qa/sessions/:session_id/messages", handler.GetQAMessages)
	}

	return r
}
