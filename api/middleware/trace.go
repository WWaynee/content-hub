package middleware

import (
	"crypto/rand"
	"encoding/hex"

	"github.com/gin-gonic/gin"
)

const TraceIDHeader = "X-Trace-Id"

// Trace 中间件：为每个请求生成或透传 trace_id，并注入 ctx。
// 下一跳（service/storage/agent）从 ctx 取 trace_id 贯穿日志。
func Trace() gin.HandlerFunc {
	return func(c *gin.Context) {
		traceID := c.GetHeader(TraceIDHeader)
		if traceID == "" {
			traceID = newTraceID()
		}
		c.Set("trace_id", traceID)
		c.Header(TraceIDHeader, traceID)
		c.Writer.Header().Set(TraceIDHeader, traceID)
		c.Next()
	}
}

// GetTraceID 从 Gin Context 取 trace_id。
func GetTraceID(c *gin.Context) string {
	if v, ok := c.Get("trace_id"); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func newTraceID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
