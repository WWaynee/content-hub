package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/WWaynee/content-hub/api/response"
	"github.com/WWaynee/content-hub/observability"
	"github.com/WWaynee/content-hub/util"
)

// JWTAuth JWT 鉴权中间件：解析 Bearer token，校验后注入 user_id/tenant_id/role。
// 失败返回 401 并中断请求。同时把 tenant_id/user_id 种进标准 ctx 供日志读取。
func JWTAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			response.Unauthorized(c, "未提供认证信息")
			c.Abort()
			return
		}
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			response.Unauthorized(c, "认证格式错误，应为 Bearer <token>")
			c.Abort()
			return
		}
		tokenString := strings.TrimSpace(parts[1])
		claims, err := util.ParseToken(tokenString)
		if err != nil {
			response.Unauthorized(c, "token 无效或已过期")
			c.Abort()
			return
		}

		c.Set("user_id", claims.UserID)
		c.Set("tenant_id", claims.TenantID)
		c.Set("role", claims.Role)
		c.Request = c.Request.WithContext(
			observability.WithTenantUser(c.Request.Context(), claims.TenantID, claims.UserID))
		c.Next()
	}
}
