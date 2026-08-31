package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/WWaynee/content-hub/api/service"
)

// Health 健康检查：真实探活所有中间件依赖。任一 down 返回 503，全部 up 返回 200。
func Health(c *gin.Context) {
	deps := service.CheckDependencies(c.Request.Context())

	allUp := true
	for _, d := range deps {
		if d.Status != "up" {
			allUp = false
			break
		}
	}
	status := http.StatusOK
	if !allUp {
		status = http.StatusServiceUnavailable
	}
	c.JSON(status, gin.H{"status": map[bool]string{true: "ok", false: "degraded"}[allUp], "deps": deps})
}
