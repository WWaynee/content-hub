package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/WWaynee/content-hub/api/middleware"
	"github.com/WWaynee/content-hub/api/response"
	"github.com/WWaynee/content-hub/api/service"
	"github.com/WWaynee/content-hub/storage"
	"github.com/WWaynee/content-hub/storage/model"
)

// 认证相关 HTTP handler。

// RegisterTenantReq 注册租户请求。
type RegisterTenantReq struct {
	Name        string `json:"name" binding:"required,min=2,max=64"`
	AdminName   string `json:"admin_name" binding:"required,min=2,max=64"`
	AdminPasswd string `json:"admin_passwd" binding:"required,min=6,max=128"`
}

// RegisterTenant 注册租户（公开）。
func RegisterTenant(c *gin.Context) {
	var req RegisterTenantReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误")
		return
	}
	u, token, err := service.RegisterTenant(c.Request.Context(), req.Name, req.AdminName, req.AdminPasswd)
	if err != nil {
		if err == service.ErrTenantExists {
			response.BadRequest(c, "租户名已存在")
			return
		}
		if err == service.ErrUsernameExists {
			response.BadRequest(c, "用户名已存在")
			return
		}
		response.ServerError(c, "注册失败")
		return
	}
	response.Success(c, gin.H{
		"token": token,
		"user":  sanitizeUser(u),
	})
}

// LoginReq 登录请求（用户名全局唯一，不传租户ID）。
type LoginReq struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// Login 登录（公开）。
func Login(c *gin.Context) {
	var req LoginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误")
		return
	}
	u, token, err := service.Login(c.Request.Context(), req.Username, req.Password)
	if err != nil {
		if err == service.ErrAccountInvalid {
			response.Unauthorized(c, "用户名或密码错误")
			return
		}
		response.ServerError(c, "登录失败")
		return
	}
	response.Success(c, gin.H{"token": token, "user": sanitizeUser(u)})
}

// RegisterMemberReq 注册工作人员请求。
type RegisterMemberReq struct {
	Username string `json:"username" binding:"required,min=2,max=64"`
	Password string `json:"password" binding:"required,min=6,max=128"`
}

// RegisterMember 在自身租户内注册工作人员（需 admin）。私有路由。
func RegisterMember(c *gin.Context) {
	role := middleware.GetRole(c)
	if role != storage.RoleAdmin {
		response.Forbidden(c, "仅管理员可注册工作人员")
		return
	}
	var req RegisterMemberReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误")
		return
	}
	tenantID := middleware.GetTenantID(c)
	u, err := service.RegisterMember(c.Request.Context(), tenantID, req.Username, req.Password)
	if err != nil {
		if err == service.ErrUsernameExists || err == service.ErrTenantNotFound {
			response.BadRequest(c, err.Error())
			return
		}
		response.ServerError(c, "注册失败")
		return
	}
	response.Success(c, sanitizeUser(u))
}

// Profile 返回当前登录用户信息（私有）。
func Profile(c *gin.Context) {
	uid := middleware.GetUserID(c)
	tenID := middleware.GetTenantID(c)
	role := middleware.GetRole(c)
	response.Success(c, gin.H{"user_id": uid, "tenant_id": tenID, "role": role})
}

// sanitizeUser 脱敏：不返回密码哈希。
func sanitizeUser(u *model.User) gin.H {
	return gin.H{
		"id":        u.ID,
		"tenant_id": u.TenantID,
		"username":  u.Username,
		"role":      u.Role,
	}
}
