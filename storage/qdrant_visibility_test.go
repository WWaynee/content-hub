package storage

import (
	"context"
	"testing"

	"github.com/WWaynee/content-hub/config"
)

// TestVectorSearchVisibilityIsolation 向量检索层可见性平面隔离对抗（P01 验收核心）：
// 同一租户内，普通检索者(owner=A/B)在向量检索中只能命中"公库 + 本人私有库"，
// 绝不能命中他租户/其它成员/他人私有库的点。
//
// 前置：仅需 Qdrant 可达（在 storage 层直连点/检索，不依赖 MySQL/OSS/Embedding）。
// 使用一个业务里不存在的 tenant 号隔离，避免污染真实数据；结果可删亦可保留。
func TestVectorSearchVisibilityIsolation(t *testing.T) {
	if _, err := config.Load(); err != nil {
		t.Skipf("配置加载失败: %v", err)
	}
	if err := InitQdrant(DefaultVectorSize); err != nil {
		t.Skipf("Qdrant 不可用,跳过: %v", err)
	}
	ctx := context.Background()
	const dim = DefaultVectorSize
	const tenant = uint64(99999001)

	vec := func(seed float32) []float32 {
		v := make([]float32, dim)
		v[0] = seed
		return v
	}

	mk := func(fileID, owner uint64, scope string) QdrantVector {
		return QdrantVector{
			ID:          uint64(tenant)*1e6 + fileID,
			TenantID:    tenant,
			FileID:      fileID,
			Scope:       scope,
			OwnerUserID: owner,
			VersionMd5:  "v-test-p01",
			ChunkIndex:  0,
			Content:     "私密测试原文 tenant=99999001",
			ChapterTitle: "P01",
			Latest:      true,
			Vector:      vec(float32(fileID)),
		}
	}

	// 同一租户内的三个点：A 私有、B 私有、公库
	points := []QdrantVector{
		mk(1, 100, ScopePrivate),   // user A 私有
		mk(2, 200, ScopePrivate),   // user B 私有
		mk(3, 0, ScopePublic),      // 公库
	}
	if err := UpsertVectors(ctx, points); err != nil {
		t.Fatalf("写入测试点失败: %v", err)
	}

	// helper：以某 owner 身份检索并断言命中 fileID 是允许集的子集
	assertAllowed := func(owner uint64, queryVec []float32, allowed ...uint64) {
		t.Helper()
		allowedSet := map[uint64]bool{}
		for _, a := range allowed {
			allowedSet[a] = true
		}
		hits, err := SearchVectors(ctx, queryVec, tenant, owner, 50)
		if err != nil {
			t.Fatalf("检索失败(owner=%d): %v", owner, err)
		}
		if len(hits) == 0 {
			t.Fatalf("owner=%d 应至少看到 %v 命中", owner, allowed)
		}
		for _, h := range hits {
			if !allowedSet[h.FileID] {
				t.Fatalf("owner=%d 越权命中 file_id=%d（不属于其可见集合 %v）", owner, h.FileID, allowed)
			}
		}
	}

	// A 只能见 A私有(1)+公库(3)，不见 B 私有(2)
	assertAllowed(100, vec(1), 1, 3)
	// B 只能见 B私有(2)+公库(3)，不见 A 私有(1)
	assertAllowed(200, vec(2), 2, 3)
	// 无身份(ctx 无 owner=0)：只可见公库(3)，不见任何私有(1/2)
	assertAllowed(0, vec(3), 3)

	// 反向：A 用 query=B 向量，仍不得漏出 B 私有——用允许集验证不越权
	hitsB, err := SearchVectors(ctx, vec(9), tenant, 100, 50)
	if err != nil {
		t.Fatalf("A 检索异常: %v", err)
	}
	if hitsB == nil {
		t.Fatalf("A 检索无返回")
	}
	for _, h := range hitsB {
		if h.FileID == 2 {
			t.Fatalf("A 用任意 query 也不得命中 B 的私有 file_id=2")
		}
	}
	t.Logf("向量检索可见性隔离 OK：owner 判定正确")
}
