package middleware

import (
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"

	"github.com/WWaynee/content-hub/api/response"
	"github.com/WWaynee/content-hub/observability"
)

// Recovery 全局异常恢复：panic 时返回 500，记录 error 日志（含 trace_id/stack）。
func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				logger := observability.WithContext(c.Request.Context())
				logger.Error("panic 恢复",
					map[string]interface{}{"recovered": err, "path": c.FullPath(), "stack": string(debug.Stack())})
				response.FailStatus(c, http.StatusInternalServerError, response.CodeServerError, "服务器内部错误")
				c.Abort()
			}
		}()
		c.Next()
	}
}
