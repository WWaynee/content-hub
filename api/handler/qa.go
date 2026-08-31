package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/WWaynee/content-hub/api/middleware"
	"github.com/WWaynee/content-hub/api/response"
	"github.com/WWaynee/content-hub/api/service"
	"github.com/WWaynee/content-hub/storage"
)

// 知识库问答 HTTP handler。

// CreateQASession 新建问答会话。
func CreateQASession(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	userID := middleware.GetUserID(c)
	s, err := service.CreateQASession(c.Request.Context(), tenantID, userID)
	if err != nil {
		response.ServerError(c, "创建会话失败")
		return
	}
	response.Success(c, gin.H{"id": s.ID, "title": s.Title})
}

// ListQASessions 列出问答会话。
func ListQASessions(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	userID := middleware.GetUserID(c)
	list, err := storage.ListQASessions(c.Request.Context(), tenantID, userID)
	if err != nil {
		response.ServerError(c, "查询会话失败")
		return
	}
	response.Success(c, list)
}

// AskQAReq 提问请求。
type AskQAReq struct {
	Question string `json:"question" binding:"required"`
}

// AskQA 提问。
func AskQA(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	userID := middleware.GetUserID(c)
	sessionID, err := parseID(c.Param("session_id"))
	if err != nil {
		response.BadRequest(c, "无效会话 ID")
		return
	}
	var req AskQAReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误")
		return
	}
	msg, err := service.AskQABot(c.Request.Context(), tenantID, userID, sessionID, req.Question)
	if err != nil {
		response.ServerError(c, "回答失败："+err.Error())
		return
	}
	response.Success(c, gin.H{"answer": msg.Content})
}

// GetQAMessages 读取会话消息。
func GetQAMessages(c *gin.Context) {
	sessionID, err := parseID(c.Param("session_id"))
	if err != nil {
		response.BadRequest(c, "无效会话 ID")
		return
	}
	msgs, err := storage.ListQAMessages(c.Request.Context(), sessionID)
	if err != nil {
		response.ServerError(c, "查询消息失败")
		return
	}
	response.Success(c, msgs)
}
