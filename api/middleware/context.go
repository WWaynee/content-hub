package middleware

import "github.com/gin-gonic/gin"

// 安全取值：从 Gin Context 取鉴权字段，拿不到或类型不符返回零值（不 panic）。
// uint64 零值为 0，正常 id 从 1 起，可用 0 判断未取到。

func GetUserID(c *gin.Context) uint64 {
	if v, ok := c.Get("user_id"); ok {
		if id, ok := v.(uint64); ok {
			return id
		}
	}
	return 0
}

func GetTenantID(c *gin.Context) uint64 {
	if v, ok := c.Get("tenant_id"); ok {
		if id, ok := v.(uint64); ok {
			return id
		}
	}
	return 0
}

func GetRole(c *gin.Context) string {
	if v, ok := c.Get("role"); ok {
		if r, ok := v.(string); ok {
			return r
		}
	}
	return ""
}
