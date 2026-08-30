package storage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// Redis 分布式滑动窗口限流（Sorted Set + Lua 原子脚本）。
// 多实例部署计数准确；Redis 不可用时保守放行。

const (
	// RateLimitTenantKeyPrefix 租户级限流 key 前缀
	RateLimitTenantKeyPrefix = "ratelimit:tenant:"
	// RateLimitUserKeyPrefix 用户级限流 key 前缀
	RateLimitUserKeyPrefix = "ratelimit:user:"
	// RateLimitIPKeyPrefix 匿名（按 IP）限流 key 前缀
	RateLimitIPKeyPrefix = "ratelimit:ip:"
)

var rateLimitScript = `
local key = KEYS[1]
local now = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local limit = tonumber(ARGV[3])
local ttl = tonumber(ARGV[4])
local member = ARGV[5]

redis.call('ZREMRANGEBYSCORE', key, 0, now - window)
local count = redis.call('ZCARD', key)
if count >= limit then
  return 0
end
redis.call('ZADD', key, now, member)
redis.call('PEXPIRE', key, ttl)
return 1
`

// AllowRequest 判断某维度某请求是否放行。返回 true=放行，false=拦截。
func AllowRequest(ctx context.Context, keyPrefix string, identity uint64, limit int, window, keyTTL time.Duration) (bool, error) {
	if limit <= 0 {
		return true, nil
	}
	if RDB == nil {
		return true, nil // Redis 未初始化，保守放行
	}
	key := fmt.Sprintf("%s%d", keyPrefix, identity)
	nowMs := time.Now().UnixMilli()
	res, err := RDB.Eval(ctx, rateLimitScript, []string{key},
		nowMs, window.Milliseconds(), limit, keyTTL.Milliseconds(), uniqueMember(nowMs)).Int()
	if err != nil {
		return true, err // 限流组件故障保守放行
	}
	return res == 1, nil
}

func uniqueMember(nowMs int64) string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%d:%s", nowMs, hex.EncodeToString(b))
}
