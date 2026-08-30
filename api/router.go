package api

import (
	"github.com/gin-gonic/gin"

	"github.com/WWaynee/content-hub/api/handler"
	"github.com/WWaynee/content-hub/api/middleware"
)

// NewRouter 构建 Gin 路由。
// 中间件顺序：Recovery → CORS → Trace → Logger。
// 私有组挂：JWT 鉴权 + 限流。
func NewRouter() *gin.Engine {
	r := gin.New()
	r.Use(middleware.Recovery(), middleware.CORS(), middleware.Trace(), middleware.Logger())

	// 健康检查（公开）
	r.GET("/health", handler.Health)

	// 公开组：认证
	pub := r.Group("/api")
	{
		pub.POST("/tenant/register", handler.RegisterTenant)
		pub.POST("/user/login", handler.Login)
	}

	// 私有组：需登录
	priv := r.Group("/api")
	priv.Use(middleware.JWTAuth(), middleware.RateLimiter())
	{
		priv.POST("/user/register", handler.RegisterMember)
		priv.GET("/user/profile", handler.Profile)
	}

	return r
}
