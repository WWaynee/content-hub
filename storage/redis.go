package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RDB 全局 Redis 客户端（InitRedis 成功后可安全使用）。
var RDB *redis.Client

// InitRedis 初始化 Redis 客户端并 Ping 验证连通性。
func InitRedis(host string, port int, password string, db int) (*redis.Client, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", host, port),
		Password: password,
		DB:       db,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("Redis 连通性检查失败（host=%s:%d db=%d）: %w", host, port, db, err)
	}
	RDB = rdb
	return rdb, nil
}
