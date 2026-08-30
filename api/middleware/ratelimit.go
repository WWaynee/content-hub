package middleware

import (
	"time"

	"github.com/gin-gonic/gin"

	"github.com/WWaynee/content-hub/api/response"
	"github.com/WWaynee/content-hub/config"
	"github.com/WWaynee/content-hub/storage"
)

// 分布式限流：租户级 + 用户级 双层滑动窗口。
// 租户级防单租户打爆服务；用户级防单用户刷接口。超限返回 429。
// tenant/user 从 JWT 上下文取值；无 token 不进此中间件。

func tenantAllowed(c *gin.Context) bool {
	tenantID := GetTenantID(c)
	if tenantID == 0 {
		return true
	}
	rl := config.Get().RateLimit
	ok, _ := storage.AllowRequest(c.Request.Context(),
		storage.RateLimitTenantKeyPrefix, tenantID,
		rl.TenantPerMin, time.Duration(rl.WindowSec)*time.Second, time.Duration(rl.KeyTTLSec)*time.Second)
	return ok
}

func userAllowed(c *gin.Context) bool {
	userID := GetUserID(c)
	if userID == 0 {
		return true
	}
	rl := config.Get().RateLimit
	ok, _ := storage.AllowRequest(c.Request.Context(),
		storage.RateLimitUserKeyPrefix, userID,
		rl.UserPerMin, time.Duration(rl.WindowSec)*time.Second, time.Duration(rl.KeyTTLSec)*time.Second)
	return ok
}

func rateLimited(decisions ...func(*gin.Context) bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		for _, decide := range decisions {
			if !decide(c) {
				response.TooManyRequests(c, "请求过于频繁，请稍后再试")
				c.Abort()
				return
			}
		}
		c.Next()
	}
}

// RateLimiter 全局限流：租户级 + 用户级。供私有路由组挂载。
func RateLimiter() gin.HandlerFunc {
	return rateLimited(tenantAllowed, userAllowed)
}
