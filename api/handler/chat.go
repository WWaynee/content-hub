package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/WWaynee/content-hub/api/middleware"
	"github.com/WWaynee/content-hub/api/response"
	"github.com/WWaynee/content-hub/api/service"
)

// 工作区对话接口。

// ChatReq 对话请求。
type ChatReq struct {
	Message    string `json:"message" binding:"required"`
	TargetType string `json:"target_type"` // sentence/paragraph/requirement_field，可空
	TargetRef  uint64 `json:"target_ref"`
}

// WorkspaceChat 处理工作区对话（需求单/稿件阶段）。返回派发结果（逐 action 明细）。
func WorkspaceChat(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	userID := middleware.GetUserID(c)
	wid, err := parseID(c.Param("workspace_id"))
	if err != nil {
		response.BadRequest(c, "无效工作区 ID")
		return
	}
	var req ChatReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误")
		return
	}

	d := service.NewDispatcher()
	res, err := d.ProcessChat(c.Request.Context(), tenantID, userID, wid, req.Message, req.TargetType, req.TargetRef)
	if err != nil {
		response.ServerError(c, "对话处理失败："+err.Error())
		return
	}
	response.Success(c, res)
}
