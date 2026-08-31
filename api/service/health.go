package service

import (
	"context"
	"time"

	"github.com/WWaynee/content-hub/mq"
	"github.com/WWaynee/content-hub/storage"
)

// DepStatus 单个依赖的健康状态。
type DepStatus struct {
	Name   string `json:"name"`
	Status string `json:"status"` // up / down
}

// CheckDependencies 探活所有中间件依赖，返回每项状态。
func CheckDependencies(ctx context.Context) []DepStatus {
	var deps []DepStatus

	// MySQL
	if db := storage.GetDB(); db != nil {
		if sqlDB, err := db.DB(); err == nil {
			if err := sqlDB.PingContext(ctx); err == nil {
				deps = append(deps, DepStatus{Name: "mysql", Status: "up"})
			} else {
				deps = append(deps, DepStatus{Name: "mysql", Status: "down"})
			}
		} else {
			deps = append(deps, DepStatus{Name: "mysql", Status: "down"})
		}
	} else {
		deps = append(deps, DepStatus{Name: "mysql", Status: "down"})
	}

	// Redis
	if storage.RDB != nil {
		pctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if err := storage.RDB.Ping(pctx).Err(); err == nil {
			deps = append(deps, DepStatus{Name: "redis", Status: "up"})
		} else {
			deps = append(deps, DepStatus{Name: "redis", Status: "down"})
		}
	} else {
		deps = append(deps, DepStatus{Name: "redis", Status: "down"})
	}

	// Qdrant
	if storage.QdrantClient != nil {
		if _, err := storage.QdrantClient.CollectionExists(ctx, "temp_health_check"); err == nil {
			deps = append(deps, DepStatus{Name: "qdrant", Status: "up"})
		} else {
			deps = append(deps, DepStatus{Name: "qdrant", Status: "down"})
		}
	} else {
		deps = append(deps, DepStatus{Name: "qdrant", Status: "down"})
	}

	// RabbitMQ
	if mq.IsReady() {
		deps = append(deps, DepStatus{Name: "rabbitmq", Status: "up"})
	} else {
		deps = append(deps, DepStatus{Name: "rabbitmq", Status: "down"})
	}

	return deps
}
