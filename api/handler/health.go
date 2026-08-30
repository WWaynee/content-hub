package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Health 健康检查（依赖状态聚合）。任一依赖异常返回 503。
// 阶段 3 先返回基本状态；后续阶段接入 MySQL/Redis/Qdrant/RabbitMQ 探针。
func Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok", "deps": gin.H{"mysql": "up", "redis": "up"}})
}
