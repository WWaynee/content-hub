package middleware

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/WWaynee/content-hub/observability"
)

// Logger 请求日志中间件：结构化输出 method/path/status/latency + trace/tenant/user。
// 需在 Trace() 和 Context() 之后挂载，以拿到 trace_id 与租户信息。
func Logger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		latency := time.Since(start)
		logger := observability.WithContext(c.Request.Context())
		logger.Info("http_request",
			map[string]interface{}{
				"method":  c.Request.Method,
				"path":    c.FullPath(),
				"status":  c.Writer.Status(),
				"latency": latency.Milliseconds(),
			})
		// Prometheus HTTP 指标（path 用 FullPath 路由模板，低基数）
		observability.IncHTTPRequest(c.Request.Method, c.FullPath(), strconv.Itoa(c.Writer.Status()), latency.Seconds())
	}
}
