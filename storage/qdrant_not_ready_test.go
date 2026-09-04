package storage

import (
	"context"
	"errors"
	"testing"
)

// 防御性回归：检索客户端未初始化时返回哨兵错误而非 nil 指针 panic
// （best-effort 治理等上层依赖这个优雅降级，而不是让请求崩掉）。
func TestSearchVectors_NotReadySentinel(t *testing.T) {
	backup := QdrantClient
	defer func() { QdrantClient = backup }()
	QdrantClient = nil

	_, err := SearchVectors(context.Background(), []float32{0.1}, 1, 0, 3)
	if !errors.Is(err, ErrQdrantNotReady) {
		t.Fatalf("期望 ErrQdrantNotReady，got %v", err)
	}
}
